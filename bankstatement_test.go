package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestInstallmentDate(t *testing.T) {
	date := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		purchase time.Time
		n        int
		want     time.Time
	}{
		{date(2026, 7, 28), 1, date(2026, 7, 28)},
		{date(2026, 7, 28), 3, date(2026, 9, 28)},
		{date(2026, 11, 15), 4, date(2027, 2, 15)},
		{date(2026, 1, 31), 2, date(2026, 2, 28)},
	}
	for _, c := range cases {
		if got := installmentDate(c.purchase, c.n); !got.Equal(c.want) {
			t.Errorf("installmentDate(%s, %d) = %s, want %s", c.purchase.Format("2006-01-02"), c.n, got.Format("2006-01-02"), c.want.Format("2006-01-02"))
		}
	}
}

// Installment lines keep their original purchase date in the PDF; they must not
// stretch the statement period beyond a single billing cycle.
func TestParseVisaItauStatementPeriod(t *testing.T) {
	for _, file := range []string{"0399723.pdf", "0399723 2026-10 cuotas.pdf"} {
		data, err := os.ReadFile("sample_bank_statements/" + file)
		if err != nil {
			t.Log(err)
			continue
		}
		stmts, err := ParseVisaItauStatement(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		checkVisaPeriods(t, stmts)
	}
}

func checkVisaPeriods(t *testing.T, stmts []*BankStatement) {
	for _, s := range stmts {
		days := s.EndDate.Sub(s.StartDate).Hours() / 24
		t.Logf("Currency: %s, Period: %s to %s", s.Currency, s.StartDate.Format("2006-01-02"), s.EndDate.Format("2006-01-02"))
		if days > 45 {
			t.Errorf("%s period spans %.0f days", s.Currency, days)
		}
		for _, tx := range s.Transactions {
			if installmentPattern.MatchString(tx.Description) {
				t.Errorf("installment marker left in description %q", tx.Description)
			}
		}
	}
}

// Itau statements carry SALDO ANTERIOR / SALDO FINAL rows; the closing balance
// must match the running balance of the last movement.
func TestParseItauStatementBalances(t *testing.T) {
	files, _ := filepath.Glob("sample_bank_statements/Estado_De_Cuenta_*.xls")
	if len(files) == 0 {
		t.Skip("no Itau samples")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s, err := ParseItauStatement(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		t.Logf("%s: %s %s to %s, %d tx, open %v close %v", filepath.Base(f), s.Currency, s.StartDate.Format("2006-01-02"), s.EndDate.Format("2006-01-02"), len(s.Transactions), s.StartBalances, s.EndBalances)
		if len(s.StartBalances) != 1 || len(s.EndBalances) != 1 {
			t.Errorf("%s: missing opening or closing balance", f)
			continue
		}
		if s.StartDate.IsZero() || s.EndDate.IsZero() {
			t.Errorf("%s: no period", f)
		}
		if n := len(s.Transactions); n > 0 && math.Abs(s.Transactions[n-1].Balance-s.EndBalances[0].Value) > 0.005 {
			t.Errorf("%s: closing %.2f != last balance %.2f", f, s.EndBalances[0].Value, s.Transactions[n-1].Balance)
		}
	}
}

// A statement without dates must not report the whole ledger as unmatched.
func TestReconcileStatementWithoutPeriod(t *testing.T) {
	ledgerTx := []LedgerTransaction{{Date: time.Date(2021, 7, 22, 0, 0, 0, 0, time.UTC), Amount: -100}}
	r := ReconcileBankStatement(&BankStatement{Currency: "$"}, ledgerTx)
	if len(r.UnmatchedLedger) != 0 || len(r.BoundaryLedger) != 0 {
		t.Errorf("got %d unmatched, %d boundary ledger transactions", len(r.UnmatchedLedger), len(r.BoundaryLedger))
	}
}
