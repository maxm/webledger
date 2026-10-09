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

// ledgerBalance returns the ledger balance of account in the given currency
// at the start of date (entries dated on date are excluded).
func ledgerBalance(ledgerName string, account string, date time.Time, currency string) float64 {
	for _, b := range QueryLedgerAccountBalances(ledgerName, account, date) {
		if b.Currency == currency {
			return b.Value
		}
	}
	return 0
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
		query = fmt.Sprintf(`reg %s -l 'commodity == "\%s"' -F '%%(format_date(date, "%%Y-%%m-%%d")) %%t	%%P
'`, account, currency)
	} else {
		query = fmt.Sprintf(`reg %s -F '%%(format_date(date, "%%Y-%%m-%%d")) %%t	%%P
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
		amountStr, payee, _ := strings.Cut(matches[2], "\t")
		amountStr = strings.TrimSpace(amountStr)
		
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
			Date:     date,
			Account:  account,
			Amount:      amount,
			Currency:    currency,
			Description: strings.TrimSpace(payee),
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
	Currency    string
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

// boundaryDays is how close to the statement period edges an unmatched ledger
// transaction must be to be reported as likely belonging to an adjacent statement
const boundaryDays = 2

// ReconciliationResult represents the complete reconciliation result
type ReconciliationResult struct {
	Matches             []ReconciliationMatch
	UnmatchedBank       []BankTransaction
	UnmatchedLedger     []LedgerTransaction
	BoundaryLedger      []LedgerTransaction // unmatched, but dated within boundaryDays of the period edges
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
		// Statements overlap by a few days at each edge: items dated near the
		// period boundary often post on the previous or next statement.
		nearStart := !statement.StartDate.IsZero() && lt.Date.Before(statement.StartDate.AddDate(0, 0, boundaryDays+1))
		nearEnd := !statement.EndDate.IsZero() && lt.Date.After(statement.EndDate.AddDate(0, 0, -boundaryDays-1))
		if nearStart || nearEnd {
			result.BoundaryLedger = append(result.BoundaryLedger, lt)
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

// SummaryLine is one reason the ledger and the statement differ
type SummaryLine struct {
	Label  string
	Count  int
	Amount float64
}

// ReconciliationSummary compares one statement (one currency) against the
// ledger and breaks the difference down into the listed unmatched items.
// All amounts use the ledger's sign convention, so a credit card balance owed
// is negative.
type ReconciliationSummary struct {
	Currency        string
	StartDate       time.Time
	EndDate         time.Time
	StatementLabel  string
	StatementAmount float64
	StatementNote   string
	LedgerLabel     string
	LedgerNote      string
	LedgerAmount    float64
	Difference      float64 // LedgerAmount - StatementAmount
	Explained       []SummaryLine
	Unexplained     float64 // Difference not accounted for by Explained
	Matched         int
	BankCount       int
}

func (s ReconciliationSummary) Reconciled() bool  { return math.Abs(s.Difference) < 0.005 }
func (s ReconciliationSummary) Explainable() bool { return math.Abs(s.Unexplained) < 0.005 }

// SummarizeReconciliation compares the statement's closing balance with the
// ledger balance at the end of the period. When the statement has no closing
// balance it compares the net movement within the period instead.
func SummarizeReconciliation(ledgerName string, account string, stmt *BankStatement, result *ReconciliationResult) ReconciliationSummary {
	sum := ReconciliationSummary{
		Currency:  stmt.Currency,
		StartDate: stmt.StartDate,
		EndDate:   stmt.EndDate,
		Matched:   len(result.Matches),
		BankCount: len(stmt.Transactions),
	}

	var closing *Amount
	for i := range stmt.EndBalances {
		if stmt.EndBalances[i].Currency == stmt.Currency {
			closing = &stmt.EndBalances[i]
		}
	}

	periodEnd := stmt.EndDate.AddDate(0, 0, 1)
	ledgerEnd := ledgerBalance(ledgerName, account, periodEnd, stmt.Currency)

	if closing != nil {
		sum.StatementLabel = "Statement"
		sum.StatementAmount = closing.Value
		if stmt.Liability {
			sum.StatementAmount = -closing.Value
			sum.StatementNote = fmt.Sprintf("Closing balance, %s owed", FormatMoney(closing.Value, stmt.Currency))
		}
		sum.LedgerLabel = "Ledger"
		sum.LedgerNote = "Balance at end of " + stmt.EndDate.Format("2006-01-02")
		sum.LedgerAmount = ledgerEnd
	} else {
		for _, tx := range stmt.Transactions {
			sum.StatementAmount += tx.Credit - tx.Debit
		}
		sum.StatementLabel = "Statement movement"
		sum.StatementNote = "No closing balance on the statement; comparing net movement within the period"
		sum.LedgerLabel = "Ledger movement"
		sum.LedgerAmount = ledgerEnd - ledgerBalance(ledgerName, account, stmt.StartDate, stmt.Currency)
	}
	sum.Difference = sum.LedgerAmount - sum.StatementAmount

	add := func(label string, txs []LedgerTransaction, sign float64) {
		line := SummaryLine{Label: label}
		for _, lt := range txs {
			line.Count++
			line.Amount += sign * lt.Amount
		}
		if line.Count > 0 {
			sum.Explained = append(sum.Explained, line)
		}
	}

	add("Not on statement", result.UnmatchedLedger, 1)

	var missing []LedgerTransaction
	for _, bt := range result.UnmatchedBank {
		missing = append(missing, LedgerTransaction{Amount: bt.Credit - bt.Debit})
	}
	add("Missing from ledger", missing, -1)

	// Edge entries near the end are inside the ledger cut but not yet billed.
	// Near the start they were billed on the previous statement, which only
	// matters when comparing movement.
	var edgeStart, edgeEnd []LedgerTransaction
	for _, lt := range result.BoundaryLedger {
		if lt.Date.After(stmt.EndDate.AddDate(0, 0, -boundaryDays-1)) {
			edgeEnd = append(edgeEnd, lt)
		} else {
			edgeStart = append(edgeStart, lt)
		}
	}
	add("Next statement", edgeEnd, 1)

	// Matched entries whose ledger date falls outside the period are on the
	// statement but on the other side of the ledger cut.
	var matchedAfter, matchedBefore []LedgerTransaction
	for _, m := range result.Matches {
		if !m.LedgerTransaction.Date.Before(periodEnd) {
			matchedAfter = append(matchedAfter, *m.LedgerTransaction)
		} else if m.LedgerTransaction.Date.Before(stmt.StartDate) {
			matchedBefore = append(matchedBefore, *m.LedgerTransaction)
		}
	}
	add("Matched, ledger date after period", matchedAfter, -1)

	if closing == nil {
		add("Previous statement", edgeStart, 1)
		add("Matched, ledger date before period", matchedBefore, -1)
	}

	sum.Unexplained = sum.Difference
	for _, line := range sum.Explained {
		sum.Unexplained -= line.Amount
	}
	return sum
}

// FormatMoney formats an amount like ledger does: "$ -1,234.56"
func FormatMoney(value float64, currency string) string {
	if currency == "" {
		currency = "$"
	}
	if math.Abs(value) < 0.005 {
		value = 0
	}
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	whole := fmt.Sprintf("%.2f", value)
	intPart, frac := whole[:len(whole)-3], whole[len(whole)-3:]
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return currency + " " + sign + b.String() + frac
}
