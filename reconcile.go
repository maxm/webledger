package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AccountMapping represents a mapping from description patterns to ledger accounts
type AccountMapping struct {
	Patterns []string `json:"patterns"`
	Account  string   `json:"account"`
}

// AccountMappingsConfig holds all description-to-account mappings
type AccountMappingsConfig struct {
	DescriptionMappings []AccountMapping `json:"description_mappings"`
}

var accountMappings *AccountMappingsConfig

// LoadAccountMappings loads the account mappings from the config file
func LoadAccountMappings() {
	// Try to load from the same directory as the executable
	execPath, err := os.Executable()
	if err == nil {
		configPath := filepath.Join(filepath.Dir(execPath), "account_mappings.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var config AccountMappingsConfig
			if json.Unmarshal(data, &config) == nil {
				accountMappings = &config
				Log("Loaded %d account mappings from %s", len(config.DescriptionMappings), configPath)
				return
			}
		}
	}
	
	// Try current working directory
	if data, err := os.ReadFile("account_mappings.json"); err == nil {
		var config AccountMappingsConfig
		if json.Unmarshal(data, &config) == nil {
			accountMappings = &config
			Log("Loaded %d account mappings from account_mappings.json", len(config.DescriptionMappings))
			return
		}
	}
	
	Log("No account_mappings.json found, using defaults")
	accountMappings = &AccountMappingsConfig{}
}

// normalizeWhitespace collapses multiple whitespaces to a single space
func normalizeWhitespace(s string) string {
	// Use regexp to replace multiple whitespace with single space
	space := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(space.ReplaceAllString(s, " "))
}

// GetAccountForDescription returns the mapped account for a description, or the default
func GetAccountForDescription(description string, isExpense bool) string {
	if accountMappings == nil {
		LoadAccountMappings()
	}
	
	// Normalize and uppercase description for matching
	descNormalized := strings.ToUpper(normalizeWhitespace(description))
	
	for _, mapping := range accountMappings.DescriptionMappings {
		for _, pattern := range mapping.Patterns {
			patternNormalized := strings.ToUpper(normalizeWhitespace(pattern))
			if strings.Contains(descNormalized, patternNormalized) {
				return mapping.Account
			}
		}
	}
	
	// Return default account
	if isExpense {
		return "Expenses:Unknown"
	}
	return "Income:Unknown"
}

// QueryLedgerAccountBalances queries the ledger balance for an account at a specific date.
// It returns a slice of Amount, one per commodity found.
// The date is exclusive (balance as of end of previous day).
func QueryLedgerAccountBalances(ledgerName string, account string, endDate time.Time) []Amount {
	dateStr := endDate.Format("2006-01-02")
	query := fmt.Sprintf(`bal '%s' -e '%s' -F '%%T\n'`, account, dateStr)
	output := strings.TrimSpace(LedgerExec(ledgerName, query))
	if output == "" {
		return nil
	}

	var balances []Amount
	amountRegex := regexp.MustCompile(`^\s*((?:US)?\$)\s*([\-\d,\.]+)\s*$`)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := amountRegex.FindStringSubmatch(line); m != nil {
			currency := m[1]
			valStr := strings.ReplaceAll(m[2], ",", "")
			val := 0.0
			fmt.Sscanf(valStr, "%f", &val)
			balances = append(balances, Amount{Currency: currency, Value: val})
		}
	}
	return balances
}

// QueryLedgerTransactions queries ledger using CLI with optional commodity/currency filter
// Uses format: reg <account> -l "commodity == '<currency>'" -F "%(format_date(date, \"%Y-%m-%d\")) %t\n"
func QueryLedgerTransactions(ledgerName string, account string, currency string) ([]LedgerTransaction, error) {
	transactions := []LedgerTransaction{}
	
	// Build the query with commodity filter
	// Note: We use %t for total amount and format_date for YYYY-MM-DD format
	// Using single quotes around the -l and -F arguments to avoid shell escaping issues
	var query string
	if currency != "" {
		// Inside single quotes, $ doesn't need escaping for shell, but ledger needs \$ for regex
		query = fmt.Sprintf(`reg %s -l 'commodity == "\%s"' -F '%%(format_date(date, "%%Y-%%m-%%d")) %%t
'`, account, currency)
	} else {
		query = fmt.Sprintf(`reg %s -F '%%(format_date(date, "%%Y-%%m-%%d")) %%t
'`, account)
	}
	
	output := LedgerExec(ledgerName, query)
	if output == "" {
		return transactions, nil
	}
	
	// Parse output: each line is "date amount"
	// Example: 2025/01/15 $1,234.56
	lines := strings.Split(strings.TrimSpace(output), "\n")
	dateRegex := regexp.MustCompile(`^(\d{4}[/-]\d{1,2}[/-]\d{1,2})\s+(.+)$`)
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		matches := dateRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		
		dateStr := matches[1]
		amountStr := strings.TrimSpace(matches[2])
		
		// Parse date
		var date time.Time
		var err error
		for _, format := range []string{"2006/01/02", "2006-01-02"} {
			date, err = time.Parse(format, dateStr)
			if err == nil {
				break
			}
		}
		if err != nil {
			continue
		}
		
		// Parse amount
		amount := parseLedgerAmount(amountStr)
		
		transaction := LedgerTransaction{
			Date:    date,
			Account: account,
			Amount:  amount,
		}
		
		transactions = append(transactions, transaction)
	}
	
	return transactions, nil
}

// LedgerTransaction represents a transaction parsed from a ledger file
type LedgerTransaction struct {
	Date        time.Time
	Description string
	Account     string
	Amount      float64
	LineNumber  int
	RawEntry    string
}

// ReconciliationMatch represents a match between bank and ledger transactions
type ReconciliationMatch struct {
	BankTransaction   *BankTransaction
	LedgerTransaction *LedgerTransaction
	MatchScore        float64
	MatchType         string // "exact", "fuzzy", "none"
}

// BankTransactionWithStatus represents a bank transaction with its reconciliation status
type BankTransactionWithStatus struct {
	Transaction       BankTransaction
	Matched           bool
	MatchType         string // "exact", "fuzzy", ""
	MatchScore        float64
	LedgerTransaction *LedgerTransaction
}

// ReconciliationResult represents the complete reconciliation result
type ReconciliationResult struct {
	Matches             []ReconciliationMatch
	UnmatchedBank       []BankTransaction
	UnmatchedLedger     []LedgerTransaction
	AllBankTransactions []BankTransactionWithStatus
	BankStatement       *BankStatement
	DateRange           string
	TotalBankDebits     float64
	TotalBankCredits    float64
	TotalLedgerDebits   float64
	TotalLedgerCredits  float64
}

// ParseLedgerTransactions extracts transactions from a ledger file for a specific account
func ParseLedgerTransactions(ledgerContent string, account string) ([]LedgerTransaction, error) {
	transactions := []LedgerTransaction{}
	lines := strings.Split(ledgerContent, "\n")
	
	var currentDate time.Time
	var currentDescription string
	var entryStartLine int
	var currentEntry strings.Builder
	
	dateRegex := regexp.MustCompile(`^(\d{4})[/-](\d{1,2})[/-](\d{1,2})(?:\s+(.*))?$`)
	accountRegex := regexp.MustCompile(`^\s+(` + regexp.QuoteMeta(account) + `(?::\w+)?)\s+([\$US\-\d\.,\s]+)`)
	
	for lineNum, line := range lines {
		currentEntry.WriteString(line)
		currentEntry.WriteString("\n")
		
		// Check if this is a date line (start of new transaction)
		if matches := dateRegex.FindStringSubmatch(line); matches != nil {
			// Save previous entry if we were processing one
			if !currentDate.IsZero() && currentDescription != "" {
				// Process completed entry - already added accounts
			}
			
			// Start new entry
			currentEntry.Reset()
			currentEntry.WriteString(line)
			currentEntry.WriteString("\n")
			entryStartLine = lineNum + 1
			
			year := matches[1]
			month := matches[2]
			day := matches[3]
			desc := strings.TrimSpace(matches[4])
			
			dateStr := fmt.Sprintf("%s-%02s-%02s", year, month, day)
			if parsedDate, err := time.Parse("2006-01-02", dateStr); err == nil {
				currentDate = parsedDate
				currentDescription = desc
			}
			continue
		}
		
		// Check if this is an account line matching our account
		if !currentDate.IsZero() {
			if matches := accountRegex.FindStringSubmatch(line); matches != nil {
				matchedAccount := matches[1]
				amountStr := strings.TrimSpace(matches[2])
				
				// Parse the amount
				amount := parseLedgerAmount(amountStr)
				
				transaction := LedgerTransaction{
					Date:        currentDate,
					Description: currentDescription,
					Account:     matchedAccount,
					Amount:      amount,
					LineNumber:  entryStartLine,
					RawEntry:    currentEntry.String(),
				}
				
				transactions = append(transactions, transaction)
			}
		}
		
		// Reset on blank line
		if strings.TrimSpace(line) == "" {
			if !currentDate.IsZero() {
				currentDate = time.Time{}
				currentDescription = ""
				currentEntry.Reset()
			}
		}
	}
	
	return transactions, nil
}

// parseLedgerAmount parses an amount from a ledger entry
// Ledger CLI outputs amounts in US format: comma as thousands separator, dot as decimal
// Example: "$ 100,000.00" or "US$ -2,500.00"
func parseLedgerAmount(amountStr string) float64 {
	// Remove currency symbols
	amountStr = strings.TrimSpace(amountStr)
	amountStr = strings.ReplaceAll(amountStr, "$", "")
	amountStr = strings.ReplaceAll(amountStr, "US", "")
	amountStr = strings.ReplaceAll(amountStr, " ", "")
	
	// Ledger uses US format: comma for thousands, dot for decimal
	// Simply remove all commas (they're thousand separators)
	amountStr = strings.ReplaceAll(amountStr, ",", "")
	
	// Parse amount
	var amount float64
	fmt.Sscanf(amountStr, "%f", &amount)
	
	return amount
}

// ReconcileBankStatement performs reconciliation between bank statement and ledger
func ReconcileBankStatement(statement *BankStatement, ledgerTransactions []LedgerTransaction) *ReconciliationResult {
	result := &ReconciliationResult{
		Matches:         []ReconciliationMatch{},
		UnmatchedBank:   []BankTransaction{},
		UnmatchedLedger: []LedgerTransaction{},
		BankStatement:   statement,
	}
	
	if !statement.StartDate.IsZero() && !statement.EndDate.IsZero() {
		result.DateRange = fmt.Sprintf("%s to %s",
			statement.StartDate.Format("2006-01-02"),
			statement.EndDate.Format("2006-01-02"))
	}
	
	// Calculate totals
	for _, bt := range statement.Transactions {
		result.TotalBankDebits += bt.Debit
		result.TotalBankCredits += bt.Credit
	}
	
	for _, lt := range ledgerTransactions {
		if lt.Amount < 0 {
			result.TotalLedgerDebits += -lt.Amount
		} else {
			result.TotalLedgerCredits += lt.Amount
		}
	}
	
	// Track which transactions have been matched
	matchedBank := make(map[int]bool)
	matchedLedger := make(map[int]bool)
	
	// First pass: exact matches (same date + same amount)
	for bi, bt := range statement.Transactions {
		if matchedBank[bi] {
			continue
		}
		
		bankAmount := bt.Credit - bt.Debit
		
		for li, lt := range ledgerTransactions {
			if matchedLedger[li] {
				continue
			}
			
			// Check if dates match exactly
			sameDate := bt.Date.Year() == lt.Date.Year() &&
				bt.Date.Month() == lt.Date.Month() &&
				bt.Date.Day() == lt.Date.Day()
			if !sameDate {
				continue
			}
			
			// Check if amounts match (allowing for small rounding differences)
			amountDiff := math.Abs(bankAmount - lt.Amount)
			if amountDiff < 0.001 {
				// Exact match!
				match := ReconciliationMatch{
					BankTransaction:   &statement.Transactions[bi],
					LedgerTransaction: &ledgerTransactions[li],
					MatchScore:        1.0,
					MatchType:         "exact",
				}
				result.Matches = append(result.Matches, match)
				matchedBank[bi] = true
				matchedLedger[li] = true
				break
			}
		}
	}
	
	// Second pass: fuzzy matches (date within 2 days + same amount)
	for bi, bt := range statement.Transactions {
		if matchedBank[bi] {
			continue
		}
		
		bankAmount := bt.Credit - bt.Debit
		
		for li, lt := range ledgerTransactions {
			if matchedLedger[li] {
				continue
			}
			
			// Check date proximity (within 2 days)
			daysDiff := math.Abs(bt.Date.Sub(lt.Date).Hours() / 24)
			if daysDiff > 2 {
				continue
			}
			
			// Check if amounts match (allowing for small rounding differences)
			amountDiff := math.Abs(bankAmount - lt.Amount)
			if amountDiff < 0.001 {
				// Fuzzy match (date within 2 days)
				match := ReconciliationMatch{
					BankTransaction:   &statement.Transactions[bi],
					LedgerTransaction: &ledgerTransactions[li],
					MatchScore:        1.0 - (daysDiff / 3.0), // Score based on date proximity
					MatchType:         "fuzzy",
				}
				result.Matches = append(result.Matches, match)
				matchedBank[bi] = true
				matchedLedger[li] = true
				break
			}
		}
	}
	
	// Collect unmatched transactions
	for bi, bt := range statement.Transactions {
		if !matchedBank[bi] {
			result.UnmatchedBank = append(result.UnmatchedBank, bt)
		}
	}
	
	// Collect unmatched ledger transactions within the bank statement date range
	for li, lt := range ledgerTransactions {
		if matchedLedger[li] {
			continue
		}
		// Only include ledger transactions within the bank statement date range
		if !statement.StartDate.IsZero() && lt.Date.Before(statement.StartDate) {
			continue
		}
		if !statement.EndDate.IsZero() && lt.Date.After(statement.EndDate) {
			continue
		}
		result.UnmatchedLedger = append(result.UnmatchedLedger, lt)
	}
	
	// Build AllBankTransactions with status for each transaction
	for bi, bt := range statement.Transactions {
		txWithStatus := BankTransactionWithStatus{
			Transaction: bt,
			Matched:     matchedBank[bi],
		}
		
		// Find the match details if matched
		if matchedBank[bi] {
			for _, match := range result.Matches {
				if match.BankTransaction.Date == bt.Date &&
					match.BankTransaction.Description == bt.Description &&
					match.BankTransaction.Debit == bt.Debit &&
					match.BankTransaction.Credit == bt.Credit {
					txWithStatus.MatchType = match.MatchType
					txWithStatus.MatchScore = match.MatchScore
					txWithStatus.LedgerTransaction = match.LedgerTransaction
					break
				}
			}
		}
		
		result.AllBankTransactions = append(result.AllBankTransactions, txWithStatus)
	}
	
	return result
}

// groupedTransaction holds transactions grouped by date and counterpart account
type groupedTransaction struct {
	Date           time.Time
	BankAccount    string
	CounterAccount string
	Transactions   []BankTransaction
}

// GenerateLedgerEntries generates suggested ledger entries for unmatched bank transactions
// Groups transactions by date and counterpart account into single entries
func GenerateLedgerEntries(unmatchedTransactions []BankTransaction) []string {
	entries := []string{}
	
	// Group transactions by date + bank account + counter account (only for known accounts)
	groups := make(map[string]*groupedTransaction)
	var ungroupedTransactions []BankTransaction
	
	for _, tx := range unmatchedTransactions {
		amount := tx.Credit - tx.Debit
		isExpense := amount < 0
		counterAccount := GetAccountForDescription(tx.Description, isExpense)
		
		// Only group if it's a known account (not Unknown)
		if strings.Contains(counterAccount, "Unknown") {
			ungroupedTransactions = append(ungroupedTransactions, tx)
			continue
		}
		
		// Create a key for grouping: date + bank account + counter account
		key := fmt.Sprintf("%s|%s|%s", tx.Date.Format("2006/01/02"), tx.Account, counterAccount)
		
		if groups[key] == nil {
			groups[key] = &groupedTransaction{
				Date:           tx.Date,
				BankAccount:    tx.Account,
				CounterAccount: counterAccount,
				Transactions:   []BankTransaction{},
			}
		}
		groups[key].Transactions = append(groups[key].Transactions, tx)
	}
	
	// Generate individual entries for ungrouped (unknown account) transactions
	for _, tx := range ungroupedTransactions {
		dateStr := tx.Date.Format("2006/01/02")
		desc := strings.TrimSpace(tx.Description)
		if tx.Reference != "" {
			desc = desc + " - " + tx.Reference
		}
		
		amount := tx.Credit - tx.Debit
		currency := tx.Currency
		if currency == "" {
			currency = "$"
		}
		
		isExpense := amount < 0
		counterAccount := "Expenses:Unknown"
		if !isExpense {
			counterAccount = "Income:Unknown"
		}
		
		entry := fmt.Sprintf("%s %s\n  %s  %s%.2f\n  %s\n",
			dateStr, desc, tx.Account, currency, amount, counterAccount)
		entries = append(entries, entry)
	}
	
	// Generate entries for each group
	for _, group := range groups {
		var entry strings.Builder
		
		// Build description from all transactions
		var descriptions []string
		for _, tx := range group.Transactions {
			desc := strings.TrimSpace(tx.Description)
			if tx.Reference != "" {
				desc = desc + " - " + tx.Reference
			}
			descriptions = append(descriptions, desc)
		}
		
		dateStr := group.Date.Format("2006/01/02")
		
		// Use first description as main, or combine if multiple
		mainDesc := descriptions[0]
		if len(descriptions) > 1 {
			mainDesc = descriptions[0] + " (+" + fmt.Sprintf("%d", len(descriptions)-1) + " more)"
		}
		
		entry.WriteString(fmt.Sprintf("%s %s\n", dateStr, mainDesc))
		
		// Add each bank transaction line
		for _, tx := range group.Transactions {
			amount := tx.Credit - tx.Debit
			currency := tx.Currency
			if currency == "" {
				currency = "$"
			}
			entry.WriteString(fmt.Sprintf("  %s  %s%.2f", group.BankAccount, currency, amount))
			// Add comment with description if there are multiple transactions
			if len(group.Transactions) > 1 {
				shortDesc := strings.TrimSpace(tx.Description)
				if len(shortDesc) > 30 {
					shortDesc = shortDesc[:30] + "..."
				}
				entry.WriteString(fmt.Sprintf("  ; %s", shortDesc))
			}
			entry.WriteString("\n")
		}
		
		// Add the counterpart account line
		entry.WriteString(fmt.Sprintf("  %s\n", group.CounterAccount))
		
		entries = append(entries, entry.String())
	}
	
	return entries
}

// FormatReconciliationSummary creates a text summary of reconciliation
func FormatReconciliationSummary(result *ReconciliationResult) string {
	var summary strings.Builder
	
	currency := result.BankStatement.Currency
	if currency == "" {
		currency = "$"
	}
	
	summary.WriteString("Bank Reconciliation Summary\n")
	summary.WriteString("===========================\n\n")
	
	summary.WriteString(fmt.Sprintf("Account: %s\n", result.BankStatement.Account))
	summary.WriteString(fmt.Sprintf("Currency: %s\n", currency))
	summary.WriteString(fmt.Sprintf("Period: %s\n\n", result.DateRange))
	
	summary.WriteString("Totals:\n")
	summary.WriteString(fmt.Sprintf("  Bank Debits:   %s%.2f\n", currency, result.TotalBankDebits))
	summary.WriteString(fmt.Sprintf("  Bank Credits:  %s%.2f\n", currency, result.TotalBankCredits))
	summary.WriteString(fmt.Sprintf("  Ledger Debits: %s%.2f\n", currency, result.TotalLedgerDebits))
	summary.WriteString(fmt.Sprintf("  Ledger Credits:%s%.2f\n\n", currency, result.TotalLedgerCredits))
	
	summary.WriteString(fmt.Sprintf("Matched Transactions: %d\n", len(result.Matches)))
	summary.WriteString(fmt.Sprintf("  - Exact matches: %d\n", countMatchType(result.Matches, "exact")))
	summary.WriteString(fmt.Sprintf("  - Fuzzy matches: %d\n\n", countMatchType(result.Matches, "fuzzy")))
	
	summary.WriteString(fmt.Sprintf("Unmatched Bank Transactions: %d\n", len(result.UnmatchedBank)))
	summary.WriteString(fmt.Sprintf("Unmatched Ledger Transactions: %d\n", len(result.UnmatchedLedger)))
	
	return summary.String()
}

func countMatchType(matches []ReconciliationMatch, matchType string) int {
	count := 0
	for _, m := range matches {
		if m.MatchType == matchType {
			count++
		}
	}
	return count
}
