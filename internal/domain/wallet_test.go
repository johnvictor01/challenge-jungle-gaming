package domain

import (
	"errors"
	"testing"
	"time"
)

// TestWalletCreation verifica os dados iniciais e a versão da carteira.
func TestWalletCreation(t *testing.T) {
	wallet, err := NewWallet("jogador1", "BRL", 1000)
	if err != nil {
		t.Fatalf("NewWallet retornou erro: %v", err)
	}
	if wallet.ID() == "" || len(wallet.ID()) != 36 {
		t.Errorf("ID da carteira inválido: %q", wallet.ID())
	}
	if wallet.PlayerID() != "jogador1" || wallet.Currency() != "BRL" {
		t.Errorf("dados da carteira incorretos: %+v", wallet)
	}
	if wallet.Balance().Units() != 1000 || wallet.Balance().Currency() != "BRL" {
		t.Errorf("saldo inicial incorreto: %+v", wallet.Balance())
	}
	if wallet.Version() != 1 {
		t.Errorf("versão inicial incorreta: esperado 1, recebido %d", wallet.Version())
	}
	if wallet.CreatedAt().IsZero() || wallet.UpdatedAt().IsZero() || !wallet.CreatedAt().Equal(wallet.UpdatedAt()) {
		t.Errorf("datas iniciais incorretas: created=%v updated=%v", wallet.CreatedAt(), wallet.UpdatedAt())
	}
}

// TestWalletAllowsDifferentCurrenciesForPlayer verifica carteiras independentes por moeda.
func TestWalletAllowsDifferentCurrenciesForPlayer(t *testing.T) {
	brl, err := NewWallet("jogador1", "BRL", 1000)
	if err != nil {
		t.Fatal(err)
	}
	usd, err := NewWallet("jogador1", "USD", 500)
	if err != nil {
		t.Fatal(err)
	}
	if brl.ID() == usd.ID() || brl.Currency() != "BRL" || usd.Currency() != "USD" {
		t.Errorf("carteiras por moeda deveriam ser independentes: BRL=%+v USD=%+v", brl, usd)
	}
}

// TestWalletCreationRejectsInvalidData verifica jogador, moeda e saldo inválidos.
func TestWalletCreationRejectsInvalidData(t *testing.T) {
	cases := []struct {
		name     string
		playerID string
		currency string
		balance  int64
		wantErr  error
	}{
		{"jogador vazio", "", "BRL", 0, ErrInvalidPlayerID},
		{"jogador só com espaços", "   ", "BRL", 0, ErrInvalidPlayerID},
		{"moeda vazia", "jogador1", "", 0, ErrInvalidCurrency},
		{"moeda minúscula", "jogador1", "brl", 0, ErrInvalidCurrency},
		{"moeda com tamanho inválido", "jogador1", "BR", 0, ErrInvalidCurrency},
		{"saldo negativo", "jogador1", "BRL", -1, ErrNegativeBalance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWallet(tc.playerID, tc.currency, tc.balance)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("esperava %v, recebeu %v", tc.wantErr, err)
			}
		})
	}
}

// TestRehydrateWallet recupera a carteira sem mudar seus dados persistidos.
func TestRehydrateWallet(t *testing.T) {
	createdAt := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.FixedZone("UTC-3", -3*60*60))
	updatedAt := createdAt.Add(time.Hour)
	wallet, err := RehydrateWallet("wallet-1", "jogador1", Money{units: 1250, currency: "BRL"}, 4, createdAt, updatedAt)
	if err != nil {
		t.Fatalf("RehydrateWallet retornou erro: %v", err)
	}
	if wallet.ID() != "wallet-1" || wallet.PlayerID() != "jogador1" || wallet.Currency() != "BRL" {
		t.Errorf("identidade recuperada incorretamente: %+v", wallet)
	}
	if wallet.Balance().Units() != 1250 || wallet.Version() != 4 {
		t.Errorf("saldo ou versão recuperados incorretamente: %+v", wallet)
	}
	if wallet.CreatedAt().Location() != time.UTC || wallet.UpdatedAt().Location() != time.UTC {
		t.Errorf("datas deveriam estar normalizadas em UTC")
	}
}

// TestRehydrateWalletRejectsInvalidData impede carregar estado inválido do banco.
func TestRehydrateWalletRejectsInvalidData(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name      string
		id        string
		playerID  string
		balance   Money
		version   int64
		createdAt time.Time
		updatedAt time.Time
		wantErr   error
	}{
		{"ID vazio", "", "jogador1", Money{units: 1, currency: "BRL"}, 1, now, now, ErrInvalidWalletID},
		{"jogador vazio", "wallet-1", "", Money{units: 1, currency: "BRL"}, 1, now, now, ErrInvalidPlayerID},
		{"jogador só com espaços", "wallet-1", "  ", Money{units: 1, currency: "BRL"}, 1, now, now, ErrInvalidPlayerID},
		{"moeda inválida", "wallet-1", "jogador1", Money{units: 1, currency: "brl"}, 1, now, now, ErrInvalidCurrency},
		{"saldo negativo", "wallet-1", "jogador1", Money{units: -1, currency: "BRL"}, 1, now, now, ErrNegativeBalance},
		{"versão inválida", "wallet-1", "jogador1", Money{units: 1, currency: "BRL"}, 0, now, now, ErrInvalidVersion},
		{"data de criação ausente", "wallet-1", "jogador1", Money{units: 1, currency: "BRL"}, 1, time.Time{}, now, ErrInvalidTimestamps},
		{"atualização antes da criação", "wallet-1", "jogador1", Money{units: 1, currency: "BRL"}, 1, now, now.Add(-time.Second), ErrInvalidTimestamps},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RehydrateWallet(tc.id, tc.playerID, tc.balance, tc.version, tc.createdAt, tc.updatedAt)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("esperava %v, recebeu %v", tc.wantErr, err)
			}
		})
	}
}

// TestWalletCredit soma o valor, atualiza a versão e a data de atualização.
func TestWalletCredit(t *testing.T) {
	wallet, err := NewWallet("jogador1", "BRL", 1000)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := wallet.CreatedAt()
	if err := wallet.Credit(Money{units: 250, currency: "BRL"}); err != nil {
		t.Fatalf("Credit retornou erro: %v", err)
	}
	if wallet.Balance().Units() != 1250 || wallet.Version() != 2 {
		t.Errorf("carteira após crédito incorreta: saldo=%d versão=%d", wallet.Balance().Units(), wallet.Version())
	}
	if wallet.UpdatedAt().Before(createdAt) {
		t.Errorf("UpdatedAt não pode ser anterior à criação")
	}
}

// TestWalletDebit subtrai o valor e permite usar todo o saldo disponível.
func TestWalletDebit(t *testing.T) {
	wallet, err := NewWallet("jogador1", "BRL", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := wallet.Debit(Money{units: 1000, currency: "BRL"}); err != nil {
		t.Fatalf("Debit retornou erro: %v", err)
	}
	if wallet.Balance().Units() != 0 || wallet.Version() != 2 {
		t.Errorf("carteira após débito incorreta: saldo=%d versão=%d", wallet.Balance().Units(), wallet.Version())
	}
}

// TestWalletDebitRejectsInsufficientFunds garante que saldo e versão não mudam na recusa.
func TestWalletDebitRejectsInsufficientFunds(t *testing.T) {
	wallet, err := NewWallet("jogador1", "BRL", 1000)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := wallet.UpdatedAt()
	err = wallet.Debit(Money{units: 1001, currency: "BRL"})
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("esperava ErrInsufficientFunds, recebeu %v", err)
	}
	if wallet.Balance().Units() != 1000 || wallet.Version() != 1 || !wallet.UpdatedAt().Equal(updatedAt) {
		t.Errorf("carteira mudou após débito recusado: %+v", wallet)
	}
}

// TestWalletMovementRejectsInvalidAmount verifica moeda incompatível e valor negativo.
func TestWalletMovementRejectsInvalidAmount(t *testing.T) {
	wallet, err := NewWallet("jogador1", "BRL", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := wallet.Credit(Money{units: 100, currency: "USD"}); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Credit: esperava ErrCurrencyMismatch, recebeu %v", err)
	}
	if err := wallet.Debit(Money{units: 100, currency: "USD"}); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Debit: esperava ErrCurrencyMismatch, recebeu %v", err)
	}
	if err := wallet.Credit(Money{units: -1, currency: "BRL"}); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("Credit: esperava ErrInvalidAmount, recebeu %v", err)
	}
	if err := wallet.Debit(Money{units: -1, currency: "BRL"}); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("Debit: esperava ErrInvalidAmount, recebeu %v", err)
	}
	if wallet.Balance().Units() != 1000 || wallet.Version() != 1 {
		t.Errorf("carteira mudou após movimentação inválida: %+v", wallet)
	}
}

// TestWalletZeroMovement não muda saldo nem versão quando o valor é zero.
func TestWalletZeroMovement(t *testing.T) {
	wallet, err := NewWallet("jogador1", "BRL", 1000)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := wallet.UpdatedAt()
	zero := Money{units: 0, currency: "BRL"}
	if err := wallet.Credit(zero); err != nil {
		t.Fatalf("Credit zero retornou erro: %v", err)
	}
	if err := wallet.Debit(zero); err != nil {
		t.Fatalf("Debit zero retornou erro: %v", err)
	}
	if wallet.Balance().Units() != 1000 || wallet.Version() != 1 || !wallet.UpdatedAt().Equal(updatedAt) {
		t.Errorf("operação zero alterou a carteira: %+v", wallet)
	}
}

// TestWalletCreditRejectsOverflow garante que overflow não altera o agregado.
func TestWalletCreditRejectsOverflow(t *testing.T) {
	wallet, err := NewWallet("jogador1", "BRL", 1)
	if err != nil {
		t.Fatal(err)
	}
	err = wallet.Credit(Money{units: 1<<63 - 1, currency: "BRL"})
	if !errors.Is(err, ErrOverflow) {
		t.Fatalf("esperava ErrOverflow, recebeu %v", err)
	}
	if wallet.Balance().Units() != 1 || wallet.Version() != 1 {
		t.Errorf("carteira mudou após overflow: %+v", wallet)
	}
}

// TestWalletMovementRejectsVersionOverflow não aplica saldo se a versão esgotou.
func TestWalletMovementRejectsVersionOverflow(t *testing.T) {
	now := time.Now().UTC()
	wallet, err := RehydrateWallet("wallet-1", "jogador1", Money{units: 100, currency: "BRL"}, 1<<63-1, now, now)
	if err != nil {
		t.Fatal(err)
	}
	err = wallet.Credit(Money{units: 1, currency: "BRL"})
	if !errors.Is(err, ErrVersionOverflow) {
		t.Fatalf("esperava ErrVersionOverflow, recebeu %v", err)
	}
	if wallet.Balance().Units() != 100 || wallet.Version() != 1<<63-1 {
		t.Errorf("carteira mudou após overflow da versão: %+v", wallet)
	}
}
