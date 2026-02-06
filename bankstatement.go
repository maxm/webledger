package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/extrame/xls"
	"github.com/ledongthuc/pdf"
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
				// BROU uses "U$S" for dollars, also check for "US$" and "dolar"
				if strings.Contains(cellStr, "U$S") || strings.Contains(cellStr, "US$") || 
				   strings.Contains(cellLower, "dolar") || strings.Contains(cellLower, "dólar") ||
				   strings.Contains(cellLower, "usd") {
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
	
	// Visa Itau statements are PDF files with numeric names like 0399723.pdf
	if strings.HasSuffix(filenameLower, ".pdf") {
		return "Assets:VisaItau"
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

// ParseVisaItauStatement parses a Visa Itau credit card statement PDF file
// Returns two statements: one for Pesos, one for US Dollars
func ParseVisaItauStatement(reader io.ReaderAt, size int64) ([]*BankStatement, error) {
	pdfReader, err := pdf.NewReader(reader, size)
	if err != nil {
		return nil, fmt.Errorf("error opening PDF file: %v", err)
	}

	pesoStatement := &BankStatement{
		Account:      "Assets:VisaItau",
		Currency:     "$",
		Transactions: []BankTransaction{},
	}

	dollarStatement := &BankStatement{
		Account:      "Assets:VisaItau",
		Currency:     "US$",
		Transactions: []BankTransaction{},
	}

	// Date pattern: DD MM YY
	datePattern := regexp.MustCompile(`^\s*(\d{2})\s+(\d{2})\s+(\d{2})\s+`)
	// Amount pattern: numbers with comma as decimal separator, optional negative
	amountPattern := regexp.MustCompile(`-?\d+(?:\.\d{3})*,\d{2}`)

	for pageNum := 1; pageNum <= pdfReader.NumPage(); pageNum++ {
		page := pdfReader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		texts := page.Content().Text

		// Sort texts by Y (descending = top to bottom), then X
		sort.Slice(texts, func(i, j int) bool {
			if texts[i].Y != texts[j].Y {
				return texts[i].Y > texts[j].Y
			}
			return texts[i].X < texts[j].X
		})

		// Group texts into lines
		type textLine struct {
			y     float64
			texts []pdf.Text
		}
		var lines []textLine
		var currentLine textLine
		currentLine.y = -1

		for _, t := range texts {
			if currentLine.y < 0 {
				currentLine.y = t.Y
				currentLine.texts = []pdf.Text{t}
			} else if currentLine.y-t.Y > 3 { // new line threshold
				if len(currentLine.texts) > 0 {
					lines = append(lines, currentLine)
				}
				currentLine = textLine{y: t.Y, texts: []pdf.Text{t}}
			} else {
				currentLine.texts = append(currentLine.texts, t)
			}
		}
		if len(currentLine.texts) > 0 {
			lines = append(lines, currentLine)
		}

		// Process each line
		for _, line := range lines {
			// Sort texts in line by X position
			sort.Slice(line.texts, func(i, j int) bool {
				return line.texts[i].X < line.texts[j].X
			})

			// Reconstruct full line text
			var fullText strings.Builder
			for _, t := range line.texts {
				fullText.WriteString(t.S)
			}
			lineStr := fullText.String()

			// Check if this is a transaction line (starts with date)
			if !datePattern.MatchString(lineStr) {
				continue
			}

			// Extract date
			dateMatch := datePattern.FindStringSubmatch(lineStr)
			if dateMatch == nil {
				continue
			}
			day, _ := strconv.Atoi(dateMatch[1])
			month, _ := strconv.Atoi(dateMatch[2])
			year, _ := strconv.Atoi(dateMatch[3])
			year += 2000 // Convert YY to YYYY

			date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

			// Extract description - text after date and before amounts
			// Remove the date portion first
			lineAfterDate := datePattern.ReplaceAllString(lineStr, "")

			// Find all amounts in the line and their positions
			amounts := amountPattern.FindAllString(lineAfterDate, -1)
			amountPositions := amountPattern.FindAllStringIndex(lineAfterDate, -1)

			// Extract description - everything before the first amount
			description := lineAfterDate
			if len(amounts) > 0 {
				firstAmountIdx := strings.Index(lineAfterDate, amounts[0])
				if firstAmountIdx > 0 {
					description = strings.TrimSpace(lineAfterDate[:firstAmountIdx])
				}
			}

			// Clean up description - remove reference code if present
			descParts := strings.SplitN(description, " ", 2)
			if len(descParts) == 2 && len(descParts[0]) == 4 {
				// First part is likely a 4-digit reference code
				_, err := strconv.Atoi(descParts[0])
				if err == nil {
					description = strings.TrimSpace(descParts[1])
				}
			}

			// Determine currency based on line length:
			// - len >= 115: Dollar transactions (has both origin and dollar amount columns)
			// - len < 115: Peso transactions (shorter lines, peso-only column)
			lineLen := len(lineStr)
			isDollarLine := lineLen >= 115

			// Special case: PAGOS line has BOTH peso and dollar payments
			// The peso amount is in the peso column (~char 70-85) and dollar amount is last
			isPagosLine := strings.Contains(strings.ToUpper(description), "PAGOS")

			if isPagosLine && len(amounts) >= 2 && len(amountPositions) >= 2 {
				// For PAGOS: check if there's an amount in the peso column position
				// The peso column ends around char 85-90 in the lineAfterDate
				// If the second-to-last amount ends before char 95, it's likely a peso amount
				secondLastPos := amountPositions[len(amountPositions)-2]
				if secondLastPos[1] < 95 {
					// We have both peso and dollar payments
					pesoAmount := parseVisaAmount(amounts[len(amounts)-2])
					dollarAmount := parseVisaAmount(amounts[len(amounts)-1])

					// Create peso transaction
					if pesoAmount != 0 {
						pesoTx := BankTransaction{
							Date:        date,
							Description: description,
							Account:     "Assets:VisaItau",
							Currency:    "$",
						}
						if pesoAmount < 0 {
							pesoTx.Credit = -pesoAmount
						} else {
							pesoTx.Debit = pesoAmount
						}
						pesoStatement.Transactions = append(pesoStatement.Transactions, pesoTx)
					}

					// Create dollar transaction
					if dollarAmount != 0 {
						dollarTx := BankTransaction{
							Date:        date,
							Description: description,
							Account:     "Assets:VisaItau",
							Currency:    "US$",
						}
						if dollarAmount < 0 {
							dollarTx.Credit = -dollarAmount
						} else {
							dollarTx.Debit = dollarAmount
						}
						dollarStatement.Transactions = append(dollarStatement.Transactions, dollarTx)
					}

					// Update date ranges for both
					if pesoStatement.StartDate.IsZero() || date.Before(pesoStatement.StartDate) {
						pesoStatement.StartDate = date
					}
					if pesoStatement.EndDate.IsZero() || date.After(pesoStatement.EndDate) {
						pesoStatement.EndDate = date
					}
					if dollarStatement.StartDate.IsZero() || date.Before(dollarStatement.StartDate) {
						dollarStatement.StartDate = date
					}
					if dollarStatement.EndDate.IsZero() || date.After(dollarStatement.EndDate) {
						dollarStatement.EndDate = date
					}
					continue // Skip the normal processing
				}
			}

			// Normal case: take the last amount as the statement amount
			var statementAmount float64
			if len(amounts) > 0 {
				statementAmount = parseVisaAmount(amounts[len(amounts)-1])
			}

			// Skip if no valid amount found
			if statementAmount == 0 {
				continue
			}

			// Create transaction
			tx := BankTransaction{
				Date:        date,
				Description: description,
				Account:     "Assets:VisaItau",
			}

			if isDollarLine {
				tx.Currency = "US$"
			} else {
				tx.Currency = "$"
			}

			// For credit cards: positive amounts are charges (debits)
			// Negative amounts are credits/payments
			if statementAmount < 0 {
				tx.Credit = -statementAmount
				tx.Debit = 0
			} else {
				tx.Debit = statementAmount
				tx.Credit = 0
			}

			// Add to appropriate statement
			var targetStatement *BankStatement
			if isDollarLine {
				targetStatement = dollarStatement
			} else {
				targetStatement = pesoStatement
			}

			targetStatement.Transactions = append(targetStatement.Transactions, tx)

			if targetStatement.StartDate.IsZero() || date.Before(targetStatement.StartDate) {
				targetStatement.StartDate = date
			}
			if targetStatement.EndDate.IsZero() || date.After(targetStatement.EndDate) {
				targetStatement.EndDate = date
			}
		}
	}

	var result []*BankStatement
	if len(pesoStatement.Transactions) > 0 {
		result = append(result, pesoStatement)
	}
	if len(dollarStatement.Transactions) > 0 {
		result = append(result, dollarStatement)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no transactions found in PDF")
	}

	return result, nil
}

// parseVisaAmount parses an amount string from a Visa statement (European format: 1.234,56)
func parseVisaAmount(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	
	// Remove thousands separators (periods) and convert decimal comma to period
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	
	if negative {
		return -val
	}
	return val
}
