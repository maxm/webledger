package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/extrame/xls"
)

// BankTransaction represents a single transaction from a bank statement
type BankTransaction struct {
	Date        time.Time
	Description string
	Debit       float64
	Credit      float64
	Balance     float64
	Reference   string
	Account     string // "Assets:Bank:BROU" or "Assets:Bank:Itau"
	Currency    string // "$" for Pesos, "US$" for US Dollars
}

// BankStatement represents a complete bank statement
type BankStatement struct {
	Account      string
	Currency     string // "$" for Pesos, "US$" for US Dollars
	Transactions []BankTransaction
	StartBalance float64
	EndBalance   float64
	StartDate    time.Time
	EndDate      time.Time
}

// ParseBrouStatement parses a BROU bank statement XLS file
func ParseBrouStatement(reader io.ReadSeeker) (*BankStatement, error) {
	xlsFile, err := xls.OpenReader(reader, "utf-8")
	if err != nil {
		return nil, fmt.Errorf("error opening XLS file: %v", err)
	}

	numSheets := xlsFile.NumSheets()
	if numSheets == 0 {
		return nil, fmt.Errorf("no sheets found in XLS file")
	}

	// Try each sheet to find transaction data
	for sheetIdx := 0; sheetIdx < numSheets; sheetIdx++ {
		sheet := xlsFile.GetSheet(sheetIdx)
		if sheet == nil {
			continue
		}
		
		statement, err := parseBrouSheet(sheet)
		if err == nil && len(statement.Transactions) > 0 {
			return statement, nil
		}
	}

	return nil, fmt.Errorf("no transaction data found in any sheet")
}

func parseBrouSheet(sheet *xls.WorkSheet) (*BankStatement, error) {

	statement := &BankStatement{
		Account:      "Assets:Bank:BROU",
		Currency:     "$", // Default to Pesos, will detect from sheet
		Transactions: []BankTransaction{},
	}

	// Parse the sheet looking for transaction data
	var headerRow int = -1
	var dateCol, descCol, refCol, debitCol, creditCol int = -1, -1, -1, -1, -1

	// First pass: find header row and column indices
	maxRow := int(sheet.MaxRow)
	for i := 0; i < maxRow && i < 100; i++ {
		var row *xls.Row
		func() {
			defer func() {
				if err := recover(); err != nil {
					row = nil
				}
			}()
			row = sheet.Row(i)
		}()
		
		if row == nil {
			continue
		}
		
		// Safely get last column index
		lastCol := 0
		func() {
			defer func() {
				if recover() != nil {
					lastCol = 0
				}
			}()
			lastCol = row.LastCol()
		}()
		
		if lastCol == 0 {
			continue
		}

		// Check if this is the header row
		for colIdx := 0; colIdx < lastCol; colIdx++ {
			cellValue := row.Col(colIdx)
			cellStr := strings.TrimSpace(cellValue)
			
			// Detect currency from "Moneda" field or currency indicators
			cellLower := strings.ToLower(cellStr)
			if strings.Contains(cellLower, "moneda") {
				if strings.Contains(cellStr, "US$") || strings.Contains(cellLower, "dolar") || strings.Contains(cellLower, "usd") {
					statement.Currency = "US$"
				} else if strings.Contains(cellStr, "$") || strings.Contains(cellLower, "peso") {
					statement.Currency = "$"
				}
			}

			// Look for header row with column names (not date stamps)
			if strings.EqualFold(cellStr, "fecha") {
				headerRow = i
				dateCol = colIdx
			} else if strings.Contains(strings.ToLower(cellStr), "descripci") {
				descCol = colIdx
			} else if strings.Contains(strings.ToLower(cellStr), "referencia") || strings.Contains(strings.ToLower(cellStr), "asunto") {
				refCol = colIdx
			} else if strings.Contains(strings.ToLower(cellStr), "débito") || strings.Contains(strings.ToLower(cellStr), "debito") {
				debitCol = colIdx
			} else if strings.Contains(strings.ToLower(cellStr), "crédito") || strings.Contains(strings.ToLower(cellStr), "credito") {
				creditCol = colIdx
			}
		}

		if headerRow >= 0 {
			break
		}
	}

	if headerRow == -1 {
		return nil, fmt.Errorf("could not find header row in BROU statement")
	}

	// Second pass: parse transaction data
	for i := headerRow + 1; i < maxRow; i++ {
		var row *xls.Row
		func() {
			defer func() {
				if err := recover(); err != nil {
					row = nil
				}
			}()
			row = sheet.Row(i)
		}()
		
		if row == nil {
			continue
		}
		
		// Safely get last column index
		lastCol := 0
		func() {
			defer func() {
				if recover() != nil {
					lastCol = 0
				}
			}()
			lastCol = row.LastCol()
		}()
		
		if lastCol == 0 {
			continue
		}

		dateStr := ""
		if dateCol >= 0 && dateCol < lastCol {
			dateStr = strings.TrimSpace(row.Col(dateCol))
		}

		// Stop if we hit an empty date or summary section
		if dateStr == "" || strings.Contains(strings.ToLower(dateStr), "total") {
			break
		}
		
		// Try to parse the date - skip if it's not a valid date
		date, err := parseBrouDate(dateStr)
		if err != nil {
			// Skip rows that don't have valid dates
			continue
		}

		desc := ""
		if descCol >= 0 && descCol < lastCol {
			desc = strings.TrimSpace(row.Col(descCol))
		}

		ref := ""
		if refCol >= 0 && refCol < lastCol {
			ref = strings.TrimSpace(row.Col(refCol))
		}

		debitStr := ""
		if debitCol >= 0 && debitCol < lastCol {
			debitStr = strings.TrimSpace(row.Col(debitCol))
		}

		creditStr := ""
		if creditCol >= 0 && creditCol < lastCol {
			creditStr = strings.TrimSpace(row.Col(creditCol))
		}

		debit := parseAmount(debitStr)
		credit := parseAmount(creditStr)

		transaction := BankTransaction{
			Date:        date,
			Description: desc,
			Debit:       debit,
			Credit:      credit,
			Reference:   ref,
			Account:     "Assets:Bank:BROU",
			Currency:    statement.Currency,
		}

		statement.Transactions = append(statement.Transactions, transaction)

		if statement.StartDate.IsZero() || date.Before(statement.StartDate) {
			statement.StartDate = date
		}
		if statement.EndDate.IsZero() || date.After(statement.EndDate) {
			statement.EndDate = date
		}
	}

	return statement, nil
}

// ParseItauStatement parses an Itau bank statement XLS file
func ParseItauStatement(reader io.ReadSeeker) (*BankStatement, error) {
	xlsFile, err := xls.OpenReader(reader, "utf-8")
	if err != nil {
		return nil, fmt.Errorf("error opening XLS file: %v", err)
	}

	if xlsFile.NumSheets() == 0 {
		return nil, fmt.Errorf("no sheets found in XLS file")
	}

	sheet := xlsFile.GetSheet(0)
	if sheet == nil {
		return nil, fmt.Errorf("could not get first sheet")
	}

	statement := &BankStatement{
		Account:      "Assets:Bank:Itau",
		Currency:     "$", // Default to Pesos, will detect from sheet
		Transactions: []BankTransaction{},
	}

	var headerRow int = -1
	var dateCol, conceptCol, debitCol, creditCol, balanceCol, refCol int = -1, -1, -1, -1, -1, -1
	var monedaCol int = -1 // Track the "Moneda" column to get currency from next row

	maxRow := int(sheet.MaxRow)
	
	for i := 0; i < maxRow && i < 100; i++ {
		var row *xls.Row
		func() {
			defer func() {
				if err := recover(); err != nil {
					row = nil
				}
			}()
			row = sheet.Row(i)
		}()
		
		if row == nil {
			continue
		}
		
		// Safely get last column index
		lastCol := 0
		func() {
			defer func() {
				if recover() != nil {
					lastCol = 0
				}
			}()
			lastCol = row.LastCol()
		}()
		
		if lastCol == 0 {
			continue
		}

		for colIdx := 0; colIdx < lastCol; colIdx++ {
			cellValue := row.Col(colIdx)
			cellRaw := strings.TrimSpace(cellValue)
			cellStr := strings.ToLower(cellRaw)
			
			// Detect if this is the header row with "Moneda" - remember the column
			if cellStr == "moneda" {
				monedaCol = colIdx
			}
			
			// If we previously found the "Moneda" header, check this row for the currency value
			if monedaCol >= 0 && colIdx == monedaCol && cellStr != "moneda" {
				// This is the value row for the Moneda column
				if strings.Contains(cellStr, "dolar") || strings.Contains(cellStr, "dólar") ||
				   strings.Contains(cellStr, "dollar") || 
				   strings.Contains(cellRaw, "US$") || strings.Contains(cellStr, "usd") {
					statement.Currency = "US$"
				} else if strings.Contains(cellStr, "peso") || cellRaw == "$" {
					statement.Currency = "$"
				}
			}

			if cellStr == "fecha" {
				headerRow = i
				dateCol = colIdx
			} else if cellStr == "concepto" {
				conceptCol = colIdx
			} else if strings.Contains(cellStr, "débito") || cellStr == "debito" {
				debitCol = colIdx
			} else if strings.Contains(cellStr, "crédito") || cellStr == "credito" {
				creditCol = colIdx
			} else if cellStr == "saldo" {
				balanceCol = colIdx
			} else if cellStr == "referencia" {
				refCol = colIdx
			}
		}

		if headerRow >= 0 {
			break
		}
	}

	if headerRow == -1 {
		return nil, fmt.Errorf("could not find header row in Itau statement")
	}

	for i := headerRow + 1; i < maxRow; i++ {
		var row *xls.Row
		func() {
			defer func() {
				if err := recover(); err != nil {
					row = nil
				}
			}()
			row = sheet.Row(i)
		}()
		
		if row == nil {
			continue
		}
		
		// Safely get last column index
		lastCol := 0
		func() {
			defer func() {
				if recover() != nil {
					lastCol = 0
				}
			}()
			lastCol = row.LastCol()
		}()
		
		if lastCol == 0 {
			continue
		}

		dateStr := ""
		if dateCol >= 0 && dateCol < lastCol {
			dateStr = strings.TrimSpace(row.Col(dateCol))
		}

		// Stop at empty date or "SALDO FINAL"
		if dateStr == "" || strings.Contains(strings.ToUpper(dateStr), "SALDO FINAL") {
			break
		}
		
		// Skip non-date rows
		if !strings.Contains(dateStr, "/") {
			continue
		}

		// Skip "SALDO ANTERIOR"
		concept := ""
		if conceptCol >= 0 && conceptCol < lastCol {
			concept = strings.TrimSpace(row.Col(conceptCol))
		}
		if strings.Contains(strings.ToUpper(concept), "SALDO ANTERIOR") {
			continue
		}

		ref := ""
		if refCol >= 0 && refCol < lastCol {
			ref = strings.TrimSpace(row.Col(refCol))
		}

		debitStr := ""
		if debitCol >= 0 && debitCol < lastCol {
			debitStr = strings.TrimSpace(row.Col(debitCol))
		}

		creditStr := ""
		if creditCol >= 0 && creditCol < lastCol {
			creditStr = strings.TrimSpace(row.Col(creditCol))
		}

		balanceStr := ""
		if balanceCol >= 0 && balanceCol < lastCol {
			balanceStr = strings.TrimSpace(row.Col(balanceCol))
		}

		date, err := parseItauDate(dateStr)
		if err != nil {
			// Skip rows that don't have valid dates
			continue
		}

		debit := parseAmount(debitStr)
		credit := parseAmount(creditStr)
		balance := parseAmount(balanceStr)

		transaction := BankTransaction{
			Date:        date,
			Description: concept,
			Debit:       debit,
			Credit:      credit,
			Balance:     balance,
			Reference:   ref,
			Account:     "Assets:Bank:Itau",
			Currency:    statement.Currency,
		}

		statement.Transactions = append(statement.Transactions, transaction)

		if statement.StartDate.IsZero() || date.Before(statement.StartDate) {
			statement.StartDate = date
		}
		if statement.EndDate.IsZero() || date.After(statement.EndDate) {
			statement.EndDate = date
		}
	}

	return statement, nil
}

// parseBrouDate parses a date in DD/MM/YYYY format or Excel serial number
func parseBrouDate(dateStr string) (time.Time, error) {
	// First check if it's an Excel serial number (like 46048)
	if serial, err := strconv.ParseFloat(dateStr, 64); err == nil {
		// Excel epoch is December 30, 1899
		excelEpoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
		days := int(serial)
		return excelEpoch.AddDate(0, 0, days), nil
	}
	
	// Try DD/MM/YYYY format
	formats := []string{
		"02/01/2006",
		"2/1/2006",
		"02/1/2006",
		"2/01/2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse date: %s", dateStr)
}

// parseItauDate parses a date in DD/MM/YYYY format (Itau uses same format as BROU)
func parseItauDate(dateStr string) (time.Time, error) {
	return parseBrouDate(dateStr)
}

// parseAmount parses a currency amount string, handling various formats
func parseAmount(amountStr string) float64 {
	if amountStr == "" || amountStr == "-" {
		return 0.0
	}

	// Remove currency symbols and whitespace
	amountStr = strings.TrimSpace(amountStr)
	amountStr = strings.ReplaceAll(amountStr, "$", "")
	amountStr = strings.ReplaceAll(amountStr, "US", "")
	amountStr = strings.ReplaceAll(amountStr, " ", "")

	// Handle thousand separators (both . and ,)
	// In Uruguay, . is thousand separator and , is decimal separator
	// But we need to be flexible
	
	// Count dots and commas
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

	// Handle parentheses as negative
	if strings.HasPrefix(amountStr, "(") && strings.HasSuffix(amountStr, ")") {
		amountStr = "-" + strings.Trim(amountStr, "()")
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return 0.0
	}

	return amount
}

// ParseBankStatementCSV parses a CSV bank statement (generic format)
func ParseBankStatementCSV(reader io.Reader, account string) (*BankStatement, error) {
	csvReader := csv.NewReader(reader)
	csvReader.Comma = ','
	csvReader.LazyQuotes = true

	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("error reading CSV: %v", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV file is empty or has no data rows")
	}

	statement := &BankStatement{
		Account:      account,
		Currency:     "$", // Default to Pesos for CSV
		Transactions: []BankTransaction{},
	}

	// Parse header to find column indices
	header := records[0]
	dateCol, descCol, debitCol, creditCol := -1, -1, -1, -1

	for i, col := range header {
		colLower := strings.ToLower(strings.TrimSpace(col))
		if strings.Contains(colLower, "fecha") || strings.Contains(colLower, "date") {
			dateCol = i
		} else if strings.Contains(colLower, "descripci") || strings.Contains(colLower, "description") || strings.Contains(colLower, "concepto") {
			descCol = i
		} else if strings.Contains(colLower, "débito") || strings.Contains(colLower, "debito") || strings.Contains(colLower, "debit") {
			debitCol = i
		} else if strings.Contains(colLower, "crédito") || strings.Contains(colLower, "credito") || strings.Contains(colLower, "credit") {
			creditCol = i
		} else if strings.Contains(colLower, "moneda") || strings.Contains(colLower, "currency") {
			// Check first data row for currency
			if len(records) > 1 && i < len(records[1]) {
				currencyVal := strings.TrimSpace(records[1][i])
				if strings.Contains(currencyVal, "US$") || strings.Contains(strings.ToLower(currencyVal), "usd") || strings.Contains(strings.ToLower(currencyVal), "dolar") {
					statement.Currency = "US$"
				}
			}
		}
	}

	// Parse data rows
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) == 0 {
			continue
		}

		dateStr := ""
		if dateCol >= 0 && dateCol < len(row) {
			dateStr = strings.TrimSpace(row[dateCol])
		}

		if dateStr == "" {
			continue
		}

		date, err := parseBrouDate(dateStr)
		if err != nil {
			continue
		}

		desc := ""
		if descCol >= 0 && descCol < len(row) {
			desc = strings.TrimSpace(row[descCol])
		}

		debitStr := ""
		if debitCol >= 0 && debitCol < len(row) {
			debitStr = strings.TrimSpace(row[debitCol])
		}

		creditStr := ""
		if creditCol >= 0 && creditCol < len(row) {
			creditStr = strings.TrimSpace(row[creditCol])
		}

		transaction := BankTransaction{
			Date:        date,
			Description: desc,
			Debit:       parseAmount(debitStr),
			Credit:      parseAmount(creditStr),
			Account:     account,
			Currency:    statement.Currency,
		}

		statement.Transactions = append(statement.Transactions, transaction)

		if statement.StartDate.IsZero() || date.Before(statement.StartDate) {
			statement.StartDate = date
		}
		if statement.EndDate.IsZero() || date.After(statement.EndDate) {
			statement.EndDate = date
		}
	}

	return statement, nil
}

// DetectBankFromFilename attempts to detect which bank from the filename
func DetectBankFromFilename(filename string) string {
	filenameLower := strings.ToLower(filename)
	
	if strings.Contains(filenameLower, "brou") || strings.Contains(filenameLower, "detalle_movimiento") {
		return "Assets:Bank:BROU"
	}
	
	if strings.Contains(filenameLower, "itau") || strings.Contains(filenameLower, "estado_de_cuenta") {
		return "Assets:Bank:Itau"
	}
	
	return ""
}

// FormatCurrency formats an amount as currency
func FormatCurrency(amount float64) string {
	return FormatCurrencyWithSymbol(amount, "$")
}

// FormatCurrencyWithSymbol formats an amount with a specific currency symbol
func FormatCurrencyWithSymbol(amount float64, currency string) string {
	if currency == "" {
		currency = "$"
	}
	if amount < 0 {
		return fmt.Sprintf("-%s%.2f", currency, -amount)
	}
	return fmt.Sprintf("%s%.2f", currency, amount)
}
