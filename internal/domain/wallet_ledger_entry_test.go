package domain

import (
	"errors"
	"testing"
	"time"
)

func ledgerMoney(units int64) Money {
	return Money{Units: units, Currency: "BRL"}
}

// TestWalletLedgerEntryCredit verifica que saldo posterior = saldo anterior + valor.
func TestWalletLedgerEntryCredit(t *testing.T) {
	cases := []struct {
		amount, before, after int64
	}{
		{100, 0, 100},
		{50, 50, 100},
		{200, 100, 300},
	}
	for _, tc := range cases {
		entry, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionCredit, ledgerMoney(tc.amount), ledgerMoney(tc.before), ledgerMoney(tc.after))
		if err != nil {
			t.Fatalf("NewWalletLedgerEntry retornou erro: %v", err)
		}
		if entry.Direction != DirectionCredit || entry.Amount.Units != tc.amount || entry.BalanceAfter.Units != tc.after {
			t.Errorf("lançamento de crédito incorreto: %+v", entry)
		}
		if entry.CreatedAt.IsZero() || entry.CreatedAt.Location() != time.UTC {
			t.Errorf("data de criação deveria existir em UTC: %v", entry.CreatedAt)
		}
	}
}

// TestWalletLedgerEntryDebit verifica que saldo posterior = saldo anterior - valor.
func TestWalletLedgerEntryDebit(t *testing.T) {
	cases := []struct {
		amount, before, after int64
	}{
		{50, 100, 50},
		{30, 80, 50},
		{100, 200, 100},
		{100, 100, 0},
	}
	for _, tc := range cases {
		entry, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionDebit, ledgerMoney(tc.amount), ledgerMoney(tc.before), ledgerMoney(tc.after))
		if err != nil {
			t.Fatalf("NewWalletLedgerEntry retornou erro: %v", err)
		}
		if entry.Direction != DirectionDebit || entry.BalanceAfter.Units != tc.after {
			t.Errorf("lançamento de débito incorreto: %+v", entry)
		}
	}
}

// TestWalletLedgerEntryRejectsInvalidDirection aceita somente DEBIT e CREDIT.
func TestWalletLedgerEntryRejectsInvalidDirection(t *testing.T) {
	for _, direction := range []Direction{"INVALID", "TRANSFER", ""} {
		_, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", direction, ledgerMoney(100), ledgerMoney(0), ledgerMoney(100))
		if !errors.Is(err, ErrInvalidLedgerDirection) {
			t.Errorf("para direção %q esperava ErrInvalidLedgerDirection, recebeu %v", direction, err)
		}
	}
}

// TestWalletLedgerEntryRejectsZeroOrNegativeAmount exige valor movimentado positivo.
func TestWalletLedgerEntryRejectsZeroOrNegativeAmount(t *testing.T) {
	for _, amount := range []int64{0, -1} {
		_, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionCredit, ledgerMoney(amount), ledgerMoney(100), ledgerMoney(100+amount))
		if !errors.Is(err, ErrInvalidLedgerAmount) {
			t.Errorf("para valor %d esperava ErrInvalidLedgerAmount, recebeu %v", amount, err)
		}
	}
}

// TestWalletLedgerEntryRejectsDifferentCurrencies exige uma moeda igual em todos os Money.
func TestWalletLedgerEntryRejectsDifferentCurrencies(t *testing.T) {
	valid := ledgerMoney(100)
	tests := []struct {
		name          string
		amount        Money
		before, after Money
	}{
		{"valor", Money{Units: 100, Currency: "USD"}, ledgerMoney(0), ledgerMoney(100)},
		{"saldo anterior", valid, Money{Units: 0, Currency: "USD"}, ledgerMoney(200)},
		{"saldo posterior", valid, ledgerMoney(0), Money{Units: 200, Currency: "USD"}},
		{"código inválido", Money{Units: 100, Currency: "brl"}, Money{Units: 0, Currency: "brl"}, Money{Units: 200, Currency: "brl"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionCredit, tc.amount, tc.before, tc.after)
			if !errors.Is(err, ErrCurrencyMismatch) {
				t.Errorf("esperava ErrCurrencyMismatch, recebeu %v", err)
			}
		})
	}
}

// TestWalletLedgerEntryRejectsNegativeBalances não permite registrar saldo negativo.
func TestWalletLedgerEntryRejectsNegativeBalances(t *testing.T) {
	tests := []struct {
		name   string
		before Money
		after  Money
	}{
		{"anterior", ledgerMoney(-1), ledgerMoney(99)},
		{"posterior", ledgerMoney(100), ledgerMoney(-1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionDebit, ledgerMoney(1), tc.before, tc.after)
			if !errors.Is(err, ErrNegativeBalance) {
				t.Errorf("esperava ErrNegativeBalance, recebeu %v", err)
			}
		})
	}
}

// TestWalletLedgerEntryRejectsIncorrectBalanceMath confere a conta das duas direções.
func TestWalletLedgerEntryRejectsIncorrectBalanceMath(t *testing.T) {
	tests := []struct {
		name      string
		direction Direction
		amount    int64
		before    int64
		after     int64
	}{
		{"crédito", DirectionCredit, 100, 100, 150},
		{"débito", DirectionDebit, 50, 100, 60},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", tc.direction, ledgerMoney(tc.amount), ledgerMoney(tc.before), ledgerMoney(tc.after))
			if !errors.Is(err, ErrLedgerBalanceMismatch) {
				t.Errorf("esperava ErrLedgerBalanceMismatch, recebeu %v", err)
			}
		})
	}
}

// TestWalletLedgerEntryRejectsDebitBelowZero impede débito acima do saldo anterior.
func TestWalletLedgerEntryRejectsDebitBelowZero(t *testing.T) {
	_, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionDebit, ledgerMoney(101), ledgerMoney(100), ledgerMoney(0))
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("esperava ErrInsufficientFunds, recebeu %v", err)
	}
}

// TestWalletLedgerEntryRejectsCreditOverflow impede overflow no saldo posterior.
func TestWalletLedgerEntryRejectsCreditOverflow(t *testing.T) {
	_, err := NewWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionCredit, ledgerMoney(1), ledgerMoney(1<<63-1), ledgerMoney(0))
	if !errors.Is(err, ErrOverflow) {
		t.Errorf("esperava ErrOverflow, recebeu %v", err)
	}
}

// TestWalletLedgerEntryRequiresIdentifiers exige IDs do lançamento, carteira e transação.
func TestWalletLedgerEntryRequiresIdentifiers(t *testing.T) {
	tests := []struct {
		name, id, walletID, transactionID string
	}{
		{"lançamento", "", "wallet-1", "transaction-1"},
		{"carteira", "entry-1", "", "transaction-1"},
		{"transação", "entry-1", "wallet-1", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWalletLedgerEntry(tc.id, tc.walletID, tc.transactionID, DirectionCredit, ledgerMoney(100), ledgerMoney(0), ledgerMoney(100))
			if !errors.Is(err, ErrInvalidLedgerIDs) {
				t.Errorf("esperava ErrInvalidLedgerIDs, recebeu %v", err)
			}
		})
	}
}

// TestRehydrateWalletLedgerEntry recupera lançamento e data sem reaplicar a mudança.
func TestRehydrateWalletLedgerEntry(t *testing.T) {
	createdAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.FixedZone("UTC-3", -3*60*60))
	entry, err := RehydrateWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionCredit, ledgerMoney(100), ledgerMoney(0), ledgerMoney(100), createdAt)
	if err != nil {
		t.Fatalf("RehydrateWalletLedgerEntry retornou erro: %v", err)
	}
	if entry.CreatedAt.Location() != time.UTC || !entry.CreatedAt.Equal(createdAt) {
		t.Errorf("data recuperada incorretamente: %v", entry.CreatedAt)
	}
	if _, err := RehydrateWalletLedgerEntry("entry-1", "wallet-1", "transaction-1", DirectionCredit, ledgerMoney(100), ledgerMoney(0), ledgerMoney(100), time.Time{}); !errors.Is(err, ErrInvalidLedgerTimestamp) {
		t.Errorf("esperava ErrInvalidLedgerTimestamp, recebeu %v", err)
	}
}
