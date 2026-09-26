package domain

import (
	"errors"
	"strings"
	"testing"
)

func walletForTransaction(t *testing.T, balance int64) *Wallet {
	t.Helper()
	wallet, err := NewWallet("jogador1", "BRL", balance)
	if err != nil {
		t.Fatalf("NewWallet retornou erro: %v", err)
	}
	return wallet
}

func externalTransactionInput(wallet *Wallet, kind TransactionKind, amount int64) ExternalWagerInput {
	input := ExternalWagerInput{
		ID:                    "transaction-1",
		Wallet:                wallet,
		ProviderID:            "provider-1",
		ExternalTransactionID: "external-1",
		IdempotencyKey:        "idem-1",
		PayloadHash:           strings.Repeat("a", 64),
		Kind:                  kind,
		Amount:                Money{units: amount, currency: "BRL"},
		RoundID:               "round-1",
		GameID:                "game-1",
	}
	if kind == TransactionRefund || kind == TransactionRollback {
		input.ReferenceExternalTransactionID = "bet-external-1"
	}
	return input
}

// TestNewExternalWagerTransaction valida as operações recebidas do provedor.
func TestNewExternalWagerTransaction(t *testing.T) {
	cases := []struct {
		kind   TransactionKind
		amount int64
	}{
		{TransactionBet, 100},
		{TransactionWin, 100},
		{TransactionLoss, 0},
		{TransactionRefund, 100},
		{TransactionRollback, 100},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			wallet := walletForTransaction(t, 1000)
			transaction, err := NewExternalWagerTransaction(externalTransactionInput(wallet, tc.kind, tc.amount))
			if err != nil {
				t.Fatalf("NewExternalWagerTransaction retornou erro: %v", err)
			}
			if transaction.origin != TransactionExternal || transaction.status != TransactionPending {
				t.Errorf("origem ou estado inicial incorreto: %+v", transaction)
			}
			if transaction.walletID != wallet.ID() || transaction.playerID != wallet.PlayerID() || transaction.currency != wallet.Currency() {
				t.Errorf("vínculo com a carteira incorreto: %+v", transaction)
			}
			if transaction.amount.Units() != tc.amount || transaction.amount.Currency() != "BRL" {
				t.Errorf("valor da operação incorreto: %+v", transaction.amount)
			}
			if transaction.createdAt.IsZero() || transaction.updatedAt.IsZero() {
				t.Error("datas da transação não foram preenchidas")
			}
		})
	}
}

// TestNewExternalWagerTransactionRejectsInvalidData verifica campos obrigatórios e origem.
func TestNewExternalWagerTransactionRejectsInvalidData(t *testing.T) {
	cases := []struct {
		name   string
		change func(*ExternalWagerInput)
	}{
		{"sem carteira", func(input *ExternalWagerInput) { input.Wallet = nil }},
		{"sem ID", func(input *ExternalWagerInput) { input.ID = " " }},
		{"sem provedor", func(input *ExternalWagerInput) { input.ProviderID = " " }},
		{"sem ID externo", func(input *ExternalWagerInput) { input.ExternalTransactionID = "" }},
		{"sem chave idempotente", func(input *ExternalWagerInput) { input.IdempotencyKey = "" }},
		{"hash inválido", func(input *ExternalWagerInput) { input.PayloadHash = "abc" }},
		{"hash maiúsculo", func(input *ExternalWagerInput) { input.PayloadHash = strings.Repeat("A", 64) }},
		{"sem rodada", func(input *ExternalWagerInput) { input.RoundID = "" }},
		{"sem jogo", func(input *ExternalWagerInput) { input.GameID = "" }},
		{"OPENING externo", func(input *ExternalWagerInput) { input.Kind = TransactionOpening }},
		{"tipo desconhecido", func(input *ExternalWagerInput) { input.Kind = "OTHER" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := externalTransactionInput(walletForTransaction(t, 1000), TransactionBet, 100)
			tc.change(&input)
			if _, err := NewExternalWagerTransaction(input); !errors.Is(err, ErrInvalidTransaction) {
				t.Errorf("esperava ErrInvalidTransaction, recebeu %v", err)
			}
		})
	}
}

// TestExternalTransactionAmountRules verifica valores aceitos por tipo.
func TestExternalTransactionAmountRules(t *testing.T) {
	cases := []struct {
		name      string
		kind      TransactionKind
		amount    Money
		wantError error
	}{
		{"BET positivo", TransactionBet, Money{units: 100, currency: "BRL"}, nil},
		{"BET zero", TransactionBet, Money{units: 0, currency: "BRL"}, ErrInvalidTransactionAmount},
		{"WIN zero", TransactionWin, Money{units: 0, currency: "BRL"}, ErrInvalidTransactionAmount},
		{"LOSS zero", TransactionLoss, Money{units: 0, currency: "BRL"}, nil},
		{"LOSS positivo", TransactionLoss, Money{units: 1, currency: "BRL"}, ErrInvalidTransactionAmount},
		{"REFUND zero", TransactionRefund, Money{units: 0, currency: "BRL"}, ErrInvalidTransactionAmount},
		{"ROLLBACK negativo", TransactionRollback, Money{units: -1, currency: "BRL"}, ErrInvalidTransactionAmount},
		{"moeda diferente", TransactionBet, Money{units: 100, currency: "USD"}, ErrTransactionCurrency},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := externalTransactionInput(walletForTransaction(t, 1000), tc.kind, tc.amount.Units())
			input.Amount = tc.amount
			_, err := NewExternalWagerTransaction(input)
			if tc.wantError == nil {
				if err != nil {
					t.Errorf("não esperava erro, recebeu %v", err)
				}
			} else if !errors.Is(err, tc.wantError) {
				t.Errorf("esperava %v, recebeu %v", tc.wantError, err)
			}
		})
	}
}

// TestExternalTransactionReferenceRules verifica referências opcionais e obrigatórias.
func TestExternalTransactionReferenceRules(t *testing.T) {
	cases := []struct {
		name      string
		kind      TransactionKind
		reference string
		wantError bool
	}{
		{"BET sem referência", TransactionBet, "", false},
		{"BET com referência", TransactionBet, "bet-1", true},
		{"WIN sem referência", TransactionWin, "", false},
		{"WIN com referência", TransactionWin, "bet-1", false},
		{"REFUND sem referência", TransactionRefund, "", true},
		{"ROLLBACK com referência", TransactionRollback, "bet-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := externalTransactionInput(walletForTransaction(t, 1000), tc.kind, 100)
			input.ReferenceExternalTransactionID = tc.reference
			_, err := NewExternalWagerTransaction(input)
			if (err != nil) != tc.wantError {
				t.Errorf("erro = %v; quer erro? %v", err, tc.wantError)
			}
		})
	}
}

// TestNewOpeningWagerTransaction cria OPENING apenas para saldo inicial positivo.
func TestNewOpeningWagerTransaction(t *testing.T) {
	wallet := walletForTransaction(t, 2500)
	transaction, err := NewOpeningWagerTransaction("opening-1", wallet, Money{units: 2500, currency: "BRL"})
	if err != nil {
		t.Fatalf("NewOpeningWagerTransaction retornou erro: %v", err)
	}
	if transaction.origin != TransactionInternal || transaction.kind != TransactionOpening || transaction.status != TransactionProcessed {
		t.Errorf("OPENING deveria ser interna e processada: %+v", transaction)
	}
	if transaction.walletID != wallet.ID() || transaction.playerID != wallet.PlayerID() || transaction.amount.Units() != 2500 {
		t.Errorf("metadados da abertura incorretos: %+v", transaction)
	}
	if transaction.providerID != "" || transaction.externalTransactionID != "" || transaction.idempotencyKey != "" || transaction.payloadHash != "" || transaction.roundID != "" || transaction.gameID != "" {
		t.Errorf("OPENING não deve ter metadados externos: %+v", transaction)
	}
	if transaction.processedAt == nil || transaction.resultBalance == nil || transaction.resultBalance.Units() != 2500 {
		t.Errorf("resultado da abertura não foi preenchido: %+v", transaction)
	}
}

// TestNewOpeningWagerTransactionRejectsZeroAndInvalidData impede abertura incoerente.
func TestNewOpeningWagerTransactionRejectsZeroAndInvalidData(t *testing.T) {
	wallet := walletForTransaction(t, 0)
	if _, err := NewOpeningWagerTransaction("opening-1", wallet, Money{units: 0, currency: "BRL"}); !errors.Is(err, ErrInvalidTransactionAmount) {
		t.Errorf("saldo zero não deve criar OPENING, recebeu %v", err)
	}
	if _, err := NewOpeningWagerTransaction("opening-1", wallet, Money{units: 10, currency: "USD"}); !errors.Is(err, ErrTransactionCurrency) {
		t.Errorf("esperava moeda incompatível, recebeu %v", err)
	}
	if _, err := NewOpeningWagerTransaction("", wallet, Money{units: 10, currency: "BRL"}); !errors.Is(err, ErrInvalidTransaction) {
		t.Errorf("esperava transação inválida, recebeu %v", err)
	}
	if _, err := NewOpeningWagerTransaction("opening-1", wallet, Money{units: 10, currency: "BRL"}); !errors.Is(err, ErrInvalidTransaction) {
		t.Errorf("valor de OPENING precisa corresponder ao saldo inicial, recebeu %v", err)
	}
}

// TestWagerTransactionStateTransitions valida espera por referência e conclusão.
func TestWagerTransactionStateTransitions(t *testing.T) {
	wallet := walletForTransaction(t, 1000)
	input := externalTransactionInput(wallet, TransactionRefund, 100)
	transaction, err := NewExternalWagerTransaction(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.WaitForReference(); err != nil {
		t.Fatalf("WaitForReference retornou erro: %v", err)
	}
	if transaction.status != TransactionPendingReference {
		t.Fatalf("estado esperado PENDING_REFERENCE, recebido %s", transaction.status)
	}
	if err := transaction.MarkProcessed(Money{units: 900, currency: "BRL"}); !errors.Is(err, ErrInvalidTransactionState) {
		t.Fatalf("não deveria processar sem resolver referência, recebeu %v", err)
	}
	bet := WagerTransaction{
		id: "bet-internal-1", origin: TransactionExternal, walletID: wallet.ID(),
		playerID: wallet.PlayerID(), currency: "BRL", providerID: "provider-1",
		externalTransactionID: "bet-external-1", kind: TransactionBet,
		amount: Money{units: 100, currency: "BRL"}, roundID: "round-1", status: TransactionProcessed,
	}
	if err := transaction.ResumeAfterReference(bet); err != nil {
		t.Fatalf("ResumeAfterReference retornou erro: %v", err)
	}
	if transaction.status != TransactionPending || transaction.referenceTransactionID != bet.id {
		t.Errorf("referência não foi resolvida: %+v", transaction)
	}
	if err := transaction.MarkProcessed(Money{units: 900, currency: "BRL"}); err != nil {
		t.Fatalf("MarkProcessed retornou erro: %v", err)
	}
	if transaction.status != TransactionProcessed || transaction.processedAt == nil || transaction.resultBalance == nil {
		t.Errorf("transação não foi concluída corretamente: %+v", transaction)
	}
	if err := transaction.Reject("late-reject"); !errors.Is(err, ErrTerminalTransaction) {
		t.Errorf("transição após conclusão deveria ser recusada, recebeu %v", err)
	}
}

// TestWagerTransactionRejectsInvalidReference não vincula referência de outra carteira ou aposta.
func TestWagerTransactionRejectsInvalidReference(t *testing.T) {
	wallet := walletForTransaction(t, 1000)
	transaction, err := NewExternalWagerTransaction(externalTransactionInput(wallet, TransactionRefund, 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.WaitForReference(); err != nil {
		t.Fatal(err)
	}
	otherWallet := walletForTransaction(t, 1000)
	reference := WagerTransaction{
		id: "bet-internal-1", origin: TransactionExternal, walletID: otherWallet.ID(),
		playerID: wallet.PlayerID(), currency: "BRL", providerID: "provider-1",
		externalTransactionID: "bet-external-1", kind: TransactionBet,
		amount: Money{units: 100, currency: "BRL"}, roundID: "round-1", status: TransactionProcessed,
	}
	if err := transaction.ResumeAfterReference(reference); !errors.Is(err, ErrInvalidReference) {
		t.Errorf("esperava ErrInvalidReference, recebeu %v", err)
	}
	if transaction.status != TransactionPendingReference || transaction.referenceTransactionID != "" {
		t.Errorf("transação mudou após referência recusada: %+v", transaction)
	}
}

// TestWagerTransactionRejectAndFail guarda o código e fecha a transação.
func TestWagerTransactionRejectAndFail(t *testing.T) {
	for _, finish := range []struct {
		name   string
		method func(*WagerTransaction, string) error
		status TransactionStatus
	}{{"rejeitar", (*WagerTransaction).Reject, TransactionRejected}, {"falhar", (*WagerTransaction).Fail, TransactionFailed}} {
		t.Run(finish.name, func(t *testing.T) {
			transaction, err := NewExternalWagerTransaction(externalTransactionInput(walletForTransaction(t, 1000), TransactionBet, 100))
			if err != nil {
				t.Fatal(err)
			}
			if err := finish.method(transaction, "RULE_ERROR"); err != nil {
				t.Fatalf("finalização retornou erro: %v", err)
			}
			if transaction.status != finish.status || transaction.failureCode != "RULE_ERROR" || transaction.processedAt == nil {
				t.Errorf("finalização incorreta: %+v", transaction)
			}
			if err := transaction.MarkProcessed(Money{units: 1000, currency: "BRL"}); !errors.Is(err, ErrTerminalTransaction) {
				t.Errorf("transação terminal deveria ser imutável, recebeu %v", err)
			}
		})
	}
}

// TestWagerTransactionCanRejectPendingReference cobre expiração da espera pela referência.
func TestWagerTransactionCanRejectPendingReference(t *testing.T) {
	transaction, err := NewExternalWagerTransaction(externalTransactionInput(walletForTransaction(t, 1000), TransactionRefund, 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.WaitForReference(); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Reject("REFERENCE_EXPIRED"); err != nil {
		t.Fatalf("Reject retornou erro: %v", err)
	}
	if transaction.status != TransactionRejected || transaction.failureCode != "REFERENCE_EXPIRED" {
		t.Errorf("pendência não foi rejeitada: %+v", transaction)
	}
}

// TestWagerTransactionRejectsEmptyFailureCode exige um código para auditoria.
func TestWagerTransactionRejectsEmptyFailureCode(t *testing.T) {
	transaction, err := NewExternalWagerTransaction(externalTransactionInput(walletForTransaction(t, 1000), TransactionBet, 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Reject(" "); !errors.Is(err, ErrInvalidTransactionState) {
		t.Errorf("esperava erro de estado para código vazio, recebeu %v", err)
	}
	if transaction.status != TransactionPending {
		t.Errorf("transação mudou após código inválido: %s", transaction.status)
	}
}

// TestWagerTransactionRejectsInvalidProcessedBalance não conclui com saldo inválido.
func TestWagerTransactionRejectsInvalidProcessedBalance(t *testing.T) {
	transaction, err := NewExternalWagerTransaction(externalTransactionInput(walletForTransaction(t, 1000), TransactionBet, 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.MarkProcessed(Money{units: -1, currency: "BRL"}); !errors.Is(err, ErrNegativeBalance) {
		t.Errorf("esperava ErrNegativeBalance, recebeu %v", err)
	}
	if err := transaction.MarkProcessed(Money{units: 900, currency: "USD"}); !errors.Is(err, ErrTransactionCurrency) {
		t.Errorf("esperava ErrTransactionCurrency, recebeu %v", err)
	}
	if transaction.status != TransactionPending {
		t.Errorf("transação mudou após resultado inválido: %s", transaction.status)
	}
}
