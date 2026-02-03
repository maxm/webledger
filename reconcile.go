package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

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

// ReconciliationResult represents the complete reconciliation result
type ReconciliationResult struct {
	Matches             []ReconciliationMatch
	UnmatchedBank       []BankTransaction
	UnmatchedLedger     []LedgerTransaction
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
func parseLedgerAmount(amountStr string) float64 {
	// Remove currency symbols
	amountStr = strings.TrimSpace(amountStr)
	amountStr = strings.ReplaceAll(amountStr, "$", "")
	amountStr = strings.ReplaceAll(amountStr, "US", "")
	amountStr = strings.ReplaceAll(amountStr, " ", "")
	
	// Handle thousand separators
	dotCount := strings.Count(amountStr, ".")
	commaCount := strings.Count(amountStr, ",")
	
	if commaCount > 0 && dotCount > 0 {
		// Both present - dots are thousands, comma is decimal
		amountStr = strings.ReplaceAll(amountStr, ".", "")
		amountStr = strings.ReplaceAll(amountStr, ",", ".")
	} else if commaCount == 1 && dotCount == 0 {
		// Only comma - it's the decimal separator
		amountStr = strings.ReplaceAll(amountStr, ",", ".")
	} else if dotCount > 1 {
		// Multiple dots - they're thousand separators
		amountStr = strings.ReplaceAll(amountStr, ".", "")
	} else if commaCount > 1 {
		// Multiple commas - they're thousand separators
		amountStr = strings.ReplaceAll(amountStr, ",", "")
	}
	
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
	
	// First pass: exact matches (date + amount)
	for bi, bt := range statement.Transactions {
		if matchedBank[bi] {
			continue
		}
		
		bankAmount := bt.Credit - bt.Debit
		
		for li, lt := range ledgerTransactions {
			if matchedLedger[li] {
				continue
			}
			
			// Check if dates are within 3 days of each other
			daysDiff := math.Abs(bt.Date.Sub(lt.Date).Hours() / 24)
			if daysDiff > 3 {
				continue
			}
			
			// Check if amounts match (allowing for small rounding differences)
			amountDiff := math.Abs(bankAmount - lt.Amount)
			if amountDiff < 0.01 {
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
	
	// Second pass: fuzzy matches (date + similar amount + similar description)
	for bi, bt := range statement.Transactions {
		if matchedBank[bi] {
			continue
		}
		
		bankAmount := bt.Credit - bt.Debit
		bestMatchIndex := -1
		bestMatchScore := 0.0
		
		for li, lt := range ledgerTransactions {
			if matchedLedger[li] {
				continue
			}
			
			// Check date proximity (within 5 days)
			daysDiff := math.Abs(bt.Date.Sub(lt.Date).Hours() / 24)
			if daysDiff > 5 {
				continue
			}
			
			// Check amount similarity (within 5% or $10)
			amountDiff := math.Abs(bankAmount - lt.Amount)
			amountThreshold := math.Max(10.0, math.Abs(bankAmount)*0.05)
			if amountDiff > amountThreshold {
				continue
			}
			
			// Calculate match score
			dateScore := 1.0 - (daysDiff / 5.0)
			amountScore := 1.0 - (amountDiff / amountThreshold)
			descScore := CompareTransactionDescriptions(bt.Description, lt.Description)
			
			totalScore := (dateScore*0.3 + amountScore*0.5 + descScore*0.2)
			
			if totalScore > bestMatchScore && totalScore > 0.6 {
				bestMatchScore = totalScore
				bestMatchIndex = li
			}
		}
		
		if bestMatchIndex >= 0 {
			match := ReconciliationMatch{
				BankTransaction:   &statement.Transactions[bi],
				LedgerTransaction: &ledgerTransactions[bestMatchIndex],
				MatchScore:        bestMatchScore,
				MatchType:         "fuzzy",
			}
			result.Matches = append(result.Matches, match)
			matchedBank[bi] = true
			matchedLedger[bestMatchIndex] = true
		}
	}
	
	// Collect unmatched transactions
	for bi, bt := range statement.Transactions {
		if !matchedBank[bi] {
			result.UnmatchedBank = append(result.UnmatchedBank, bt)
		}
	}
	
	for li, lt := range ledgerTransactions {
		if !matchedLedger[li] {
			result.UnmatchedLedger = append(result.UnmatchedLedger, lt)
		}
	}
	
	return result
}

// GenerateLedgerEntries generates suggested ledger entries for unmatched bank transactions
func GenerateLedgerEntries(unmatchedTransactions []BankTransaction) []string {
	entries := []string{}
	
	for _, tx := range unmatchedTransactions {
		dateStr := tx.Date.Format("2006/01/02")
		desc := strings.TrimSpace(tx.Description)
		if tx.Reference != "" {
			desc = desc + " - " + tx.Reference
		}
		
		amount := tx.Credit - tx.Debit
		var amountStr string
		if amount >= 0 {
			amountStr = fmt.Sprintf("$%.2f", amount)
		} else {
			amountStr = fmt.Sprintf("$%.2f", amount)
		}
		
		// Generate entry
		entry := fmt.Sprintf("%s %s\n  %s  %s\n  Expenses:Unknown\n",
			dateStr, desc, tx.Account, amountStr)
		
		entries = append(entries, entry)
	}
	
	return entries
}

// FormatReconciliationSummary creates a text summary of reconciliation
func FormatReconciliationSummary(result *ReconciliationResult) string {
	var summary strings.Builder
	
	summary.WriteString("Bank Reconciliation Summary\n")
	summary.WriteString("===========================\n\n")
	
	summary.WriteString(fmt.Sprintf("Account: %s\n", result.BankStatement.Account))
	summary.WriteString(fmt.Sprintf("Period: %s\n\n", result.DateRange))
	
	summary.WriteString("Totals:\n")
	summary.WriteString(fmt.Sprintf("  Bank Debits:   %s\n", FormatCurrency(result.TotalBankDebits)))
	summary.WriteString(fmt.Sprintf("  Bank Credits:  %s\n", FormatCurrency(result.TotalBankCredits)))
	summary.WriteString(fmt.Sprintf("  Ledger Debits: %s\n", FormatCurrency(result.TotalLedgerDebits)))
	summary.WriteString(fmt.Sprintf("  Ledger Credits:%s\n\n", FormatCurrency(result.TotalLedgerCredits)))
	
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
