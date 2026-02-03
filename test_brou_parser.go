// +build ignore

package main

import (
	"fmt"
	"log"
	"os"
)

func Log(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func main() {
	fmt.Println("Testing BROU Statement Parser")
	fmt.Println("==============================\n")

	// Open the BROU sample file
	filename := "sample_bank_statements/Detalle_Movimiento_Cuenta.xls"
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	defer file.Close()

	fmt.Println("File opened successfully, parsing...")
	statement, err := ParseBrouStatement(file)
	if err != nil {
		log.Fatalf("Error parsing statement: %v", err)
	}

	fmt.Println("Parsing completed successfully!")
	fmt.Printf("Account: %s\n", statement.Account)
	if len(statement.Transactions) > 0 {
		fmt.Printf("Date Range: %s to %s\n",
			statement.Transactions[0].Date,
			statement.Transactions[len(statement.Transactions)-1].Date)
	}
	fmt.Printf("Total Transactions: %d\n\n", len(statement.Transactions))

	// Print all transactions
	fmt.Println("All Transactions:")
	fmt.Println("-----------------")
	for i, tx := range statement.Transactions {
		fmt.Printf("%d. Date: %s\n", i+1, tx.Date)
		fmt.Printf("   Description: %s\n", tx.Description)
		if tx.Reference != "" {
			fmt.Printf("   Reference: %s\n", tx.Reference)
		}
		fmt.Printf("   Debit:  $%.2f\n", tx.Debit)
		fmt.Printf("   Credit: $%.2f\n", tx.Credit)
		fmt.Printf("   Balance: $%.2f\n", tx.Balance)
		net := tx.Credit - tx.Debit
		fmt.Printf("   Net: $%.2f\n\n", net)
	}

	// Calculate totals
	var totalDebit, totalCredit float64
	for _, tx := range statement.Transactions {
		totalDebit += tx.Debit
		totalCredit += tx.Credit
	}

	fmt.Println("Summary:")
	fmt.Println("--------")
	fmt.Printf("Total Debits:  $%.2f\n", totalDebit)
	fmt.Printf("Total Credits: $%.2f\n", totalCredit)
	fmt.Printf("Net Change:    $%.2f\n", totalCredit-totalDebit)
}
