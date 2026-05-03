package main

import (
	"os"
	"testing"
)

func TestParseVisaItauMovimientos(t *testing.T) {
	data, err := os.ReadFile("sample_bank_statements/visaitau movimientos.txt")
	if err != nil {
		t.Fatal(err)
	}

	stmts, err := ParseVisaItauMovimientos(string(data))
	if err != nil {
		t.Fatal(err)
	}

	if len(stmts) < 1 {
		t.Fatal("expected at least 1 statement")
	}

	totalPesoTx := 0
	totalDollarTx := 0
	for _, s := range stmts {
		t.Logf("Currency: %s, Transactions: %d, Period: %s to %s",
			s.Currency, len(s.Transactions), s.StartDate.Format("2006-01-02"), s.EndDate.Format("2006-01-02"))
		for _, tx := range s.Transactions {
			if tx.Debit > 0 {
				t.Logf("  %s %-30s Debit: %.2f %s", tx.Date.Format("2006-01-02"), tx.Description, tx.Debit, tx.Currency)
			} else {
				t.Logf("  %s %-30s Credit: %.2f %s", tx.Date.Format("2006-01-02"), tx.Description, tx.Credit, tx.Currency)
			}
		}
		if s.Currency == "$" {
			totalPesoTx = len(s.Transactions)
		} else {
			totalDollarTx = len(s.Transactions)
		}
	}

	if totalPesoTx == 0 {
		t.Error("expected peso transactions")
	}
	if totalDollarTx == 0 {
		t.Error("expected dollar transactions")
	}
}
