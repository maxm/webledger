# Bank Statement Reconciliation Feature

## Overview

The bank reconciliation feature allows you to upload bank statements and automatically match them against your ledger entries to identify discrepancies. This helps ensure your ledger is complete and accurate.

## Supported Banks

### BROU (Banco de la República Oriental del Uruguay)
- **File Format**: `.xls` (Excel)
- **File Name Pattern**: `Detalle_Movimiento_Cuenta*.xls`
- **Ledger Account**: `Assets:Bank:BROU`

### Itau
- **File Format**: `.xls` (Excel)
- **File Name Pattern**: `Estado_De_Cuenta*.xls`
- **Ledger Account**: `Assets:Bank:Itau`

### Generic CSV
- **File Format**: `.csv`
- **Required Columns**: Date, Description, Debit, Credit
- **Ledger Account**: Configurable

## How to Use

1. **Navigate to Reconciliation Page**
   - Click on "Reconcile" in the navigation menu when viewing a ledger
   - Or go to `/{ledger}/reconcile`

2. **Select Bank Account**
   - Choose the bank account from the dropdown (BROU or Itau)
   - Or leave blank to auto-detect from filename

3. **Upload Statement**
   - Click "Choose File" and select your bank statement (.xls or .csv)
   - Click "Reconcile" to process

4. **Review Results**
   - **Matched Transactions**: Shows transactions that were successfully matched between bank and ledger
     - Green (Exact Match): Date and amount match perfectly
     - Yellow (Fuzzy Match): Close match based on date proximity and amount similarity
   
   - **Unmatched Bank Transactions**: Transactions in your bank statement but not in your ledger
     - These may indicate missing ledger entries
   
   - **Unmatched Ledger Transactions**: Transactions in your ledger but not in the bank statement
     - These may indicate errors or transactions that haven't cleared yet

5. **Add Missing Entries**
   - The system generates suggested ledger entries for unmatched bank transactions
   - Click "Copy to Clipboard" to copy them
   - Paste into your ledger file using the Edit page

## Matching Algorithm

The reconciliation engine uses a two-pass matching algorithm:

### Pass 1: Exact Matches
- Date within 3 days
- Amount matches within $0.001
- High confidence matches

### Pass 2: Fuzzy Matches
- Date within 5 days
- Amount within 5% or $10 (whichever is larger)
- Description similarity scoring
- Match score > 60% required

## Files Added

- **bankstatement.go**: Parser for bank statement files (BROU, Itau, CSV)
- **reconcile.go**: Reconciliation logic and matching algorithms
- **templates/views/reconcile.tmpl**: Upload form template
- **templates/views/reconcile_result.tmpl**: Results display template

## API Endpoints

### GET /{ledger}/reconcile
Displays the reconciliation upload form

### POST /{ledger}/reconcile
Processes uploaded bank statement and returns reconciliation results

**Form Parameters:**
- `statement`: Bank statement file (required)
- `account`: Bank account name (optional, auto-detected if omitted)

## Dependencies

Added `github.com/extrame/xls` for parsing Excel files.

## Example Usage

```bash
# Build the project
go build

# Run the server
./webledger

# Navigate to http://localhost:8082/ledger/{your-ledger}/reconcile
# Upload your bank statement
# Review the reconciliation results
```

## Tips

1. **Regular Reconciliation**: Reconcile monthly to catch discrepancies early
2. **Check Fuzzy Matches**: Review yellow fuzzy matches to ensure they're correct
3. **Date Tolerances**: The system allows for 3-5 day date differences to account for processing delays
4. **Multiple Currencies**: The parser handles both $ (pesos) and US$ (dollars)
5. **Thousand Separators**: Handles both comma and dot as thousand/decimal separators

## Troubleshooting

### "Could not find header row"
- The parser couldn't identify column headers in your statement
- Check that your file is the correct format
- Try downloading the statement again from your bank

### "Could not parse date"
- Date format not recognized
- Expected format: DD/MM/YYYY
- Contact maintainer if you need support for additional date formats

### No transactions parsed
- File may be empty or corrupted
- Check that the file opens correctly in Excel
- Verify you uploaded the correct file type

## Future Enhancements

Potential improvements for future versions:

- [ ] Support for additional banks (Santander, HSBC, etc.)
- [ ] PDF statement parsing
- [ ] Automatic transaction categorization using ML
- [ ] Multi-currency reconciliation improvements
- [ ] Batch reconciliation for multiple statements
- [ ] Export reconciliation results to CSV
- [ ] Historical reconciliation tracking

## Contributing

To add support for a new bank:

1. Add a parser function in `bankstatement.go` following the pattern of `ParseBrouStatement` or `ParseItauStatement`
2. Update `DetectBankFromFilename` to recognize the new bank's file patterns
3. Add the bank to the dropdown in `templates/views/reconcile.tmpl`
4. Test with sample statements from the bank
5. Update this README with the new bank's details
