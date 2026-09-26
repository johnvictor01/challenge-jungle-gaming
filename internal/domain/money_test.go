package domain

import (
	"errors"
	"testing"
)

// TestParseMoney: decimal válido vira unidades mínimas sem ponto flutuante.
func TestParseMoney(t *testing.T) {
	got, err := ParseMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("ParseMoney retornou erro: %v", err)
	}
	if got.Units != 2500 || got.Currency != "BRL" {
		t.Errorf("resultado inesperado: %+v", got)
	}
}

// TestParseMoneyRejectsInvalidAmounts: formatos externos inválidos são recusados.
func TestParseMoneyRejectsInvalidAmounts(t *testing.T) {
	for _, amount := range []string{"", "abc", "-25.00", "+1.00", "1e10", "NaN", "Infinity", "25.123", "25", ".25"} {
		t.Run(amount, func(t *testing.T) {
			if _, err := ParseMoney(amount, "BRL"); err == nil {
				t.Errorf("ParseMoney(%q) deveria retornar erro", amount)
			}
		})
	}
	if _, err := ParseMoney("1.00", "brl"); err == nil {
		t.Error("moeda fora do padrão maiúsculo deveria ser recusada")
	}
	if _, err := ParseMoney("1.00", "ZZZ"); err == nil {
		t.Error("código de moeda fora do ISO 4217 deveria ser recusado")
	}
}

// TestParseMoneyRejectsOverflow: valor que não cabe em int64 é recusado.
func TestParseMoneyRejectsOverflow(t *testing.T) {
	_, err := ParseMoney("92233720368547758.08", "BRL")
	if !errors.Is(err, ErrOverflow) {
		t.Errorf("esperava ErrOverflow, recebeu %v", err)
	}
}

// TestMoneyZero: zero continua associado à moeda informada.
func TestMoneyZero(t *testing.T) {
	got, err := ParseMoney("0.00", "BRL")
	if err != nil {
		t.Fatalf("ParseMoney retornou erro: %v", err)
	}
	if got.Units != 0 || got.Currency != "BRL" {
		t.Errorf("resultado inesperado: %+v", got)
	}
}

// TestMoneyAdd: soma valores da mesma moeda e identifica overflow.
func TestMoneyAdd(t *testing.T) {
	cases := []struct {
		a, b    int64
		want    int64
		wantErr error
	}{
		{1000, 1500, 2500, nil},
		{1, 2, 3, nil},
		{1<<63 - 1, 1, 0, ErrOverflow},
	}
	for _, tc := range cases {
		got, err := (Money{Units: tc.a, Currency: "BRL"}).Add(Money{Units: tc.b, Currency: "BRL"})
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Add(%d, %d): esperava %v, recebeu %v", tc.a, tc.b, tc.wantErr, err)
			}
			continue
		}
		if err != nil || got.Units != tc.want {
			t.Errorf("Add(%d, %d) = %+v, %v; esperado Units=%d", tc.a, tc.b, got, err, tc.want)
		}
	}
}

// TestMoneySubtract: subtração pode dar negativo internamente, mas não pode estourar.
func TestMoneySubtract(t *testing.T) {
	cases := []struct {
		a, b    int64
		want    int64
		wantErr error
	}{
		{1500, 1000, 500, nil},
		{1, 2, -1, nil},
		{-1 << 63, 1, 0, ErrOverflow},
		{1<<63 - 1, -1, 0, ErrOverflow},
	}
	for _, tc := range cases {
		got, err := (Money{Units: tc.a, Currency: "BRL"}).Subtract(Money{Units: tc.b, Currency: "BRL"})
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Subtract(%d, %d): esperava %v, recebeu %v", tc.a, tc.b, tc.wantErr, err)
			}
			continue
		}
		if err != nil || got.Units != tc.want {
			t.Errorf("Subtract(%d, %d) = %+v, %v; esperado Units=%d", tc.a, tc.b, got, err, tc.want)
		}
	}
}

// TestMoneyNegate: troca o sinal e detecta o menor int64, que não pode ser negado.
func TestMoneyNegate(t *testing.T) {
	cases := []struct {
		units   int64
		want    int64
		wantErr error
	}{{1000, -1000, nil}, {-1000, 1000, nil}, {-1 << 63, 0, ErrOverflow}}
	for _, tc := range cases {
		got, err := (Money{Units: tc.units, Currency: "BRL"}).Negate()
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Negate(%d): esperava %v, recebeu %v", tc.units, tc.wantErr, err)
			}
			continue
		}
		if err != nil || got.Units != tc.want {
			t.Errorf("Negate(%d) = %+v, %v; esperado Units=%d", tc.units, got, err, tc.want)
		}
	}
}

// TestMoneyCompare: compara valores da mesma moeda e ordena menor, igual e maior.
func TestMoneyCompare(t *testing.T) {
	cases := []struct {
		a, b int64
		want int
	}{{1000, 1500, -1}, {1500, 1000, 1}, {1000, 1000, 0}}
	for _, tc := range cases {
		got, err := (Money{Units: tc.a, Currency: "BRL"}).Compare(Money{Units: tc.b, Currency: "BRL"})
		if err != nil || got != tc.want {
			t.Errorf("Compare(%d, %d) = %d, %v; esperado %d", tc.a, tc.b, got, err, tc.want)
		}
	}
}

// TestMoneyRejectsDifferentCurrencies: aritmética e comparação exigem moedas iguais.
func TestMoneyRejectsDifferentCurrencies(t *testing.T) {
	brl := Money{Units: 100, Currency: "BRL"}
	usd := Money{Units: 100, Currency: "USD"}
	if _, err := brl.Add(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Add: esperava ErrCurrencyMismatch, recebeu %v", err)
	}
	if _, err := brl.Subtract(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Subtract: esperava ErrCurrencyMismatch, recebeu %v", err)
	}
	if _, err := brl.Compare(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Compare: esperava ErrCurrencyMismatch, recebeu %v", err)
	}
}

// TestMoneyString: formata valor positivo, zero e negativo com duas casas.
func TestMoneyString(t *testing.T) {
	cases := []struct {
		money Money
		want  string
	}{{Money{Units: 1000, Currency: "BRL"}, "10.00 BRL"},
		{Money{Units: 1, Currency: "USD"}, "0.01 USD"},
		{Money{Units: -550, Currency: "EUR"}, "-5.50 EUR"},
		{Money{Units: -1 << 63, Currency: "BRL"}, "-92233720368547758.08 BRL"}}
	for _, tc := range cases {
		if got := tc.money.String(); got != tc.want {
			t.Errorf("String() = %q; esperado %q", got, tc.want)
		}
	}
}
