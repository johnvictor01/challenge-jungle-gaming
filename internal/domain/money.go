package domain

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrCurrencyMismatch = errors.New("money currencies do not match")
	ErrOverflow         = errors.New("money arithmetic overflow")
)

// Money guarda um valor inteiro em unidades mínimas e a moeda desse valor.
// Por exemplo, 25,00 BRL é guardado como Units: 2500 e Currency: "BRL".
type Money struct {
	Units    int64
	Currency string
}

// ParseMoney lê um valor externo com exatamente duas casas decimais.
// Entradas negativas são rejeitadas; valores negativos internos podem surgir
// de operações como Subtract.
func ParseMoney(amount string, currency string) (Money, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return Money{}, errors.New("amount cannot be empty")
	}
	if len(currency) != 3 {
		return Money{}, errors.New("currency must be a three-letter ISO code")
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return Money{}, errors.New("currency must be an uppercase ISO code")
		}
	}
	if strings.HasPrefix(amount, "-") || strings.HasPrefix(amount, "+") {
		return Money{}, errors.New("amount cannot be negative or signed")
	}
	if strings.ContainsAny(amount, "eE") {
		return Money{}, errors.New("amount cannot be in scientific notation")
	}
	if strings.EqualFold(amount, "nan") || strings.EqualFold(amount, "infinity") {
		return Money{}, errors.New("amount cannot be NaN or Infinity")
	}

	parts := strings.Split(amount, ".")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) != 2 {
		return Money{}, errors.New("amount must use exactly two decimal places")
	}
	for _, digit := range parts[0] + parts[1] {
		if digit < '0' || digit > '9' {
			return Money{}, errors.New("invalid amount format")
		}
	}

	units, err := strconv.ParseInt(parts[0]+parts[1], 10, 64)
	if err != nil {
		return Money{}, ErrOverflow
	}
	return Money{Units: units, Currency: currency}, nil
}

// Add soma dois valores da mesma moeda e retorna erro se houver overflow.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	const maxInt64 = int64(1<<63 - 1)
	const minInt64 = -1 << 63
	if (other.Units > 0 && m.Units > maxInt64-other.Units) ||
		(other.Units < 0 && m.Units < minInt64-other.Units) {
		return Money{}, ErrOverflow
	}
	return Money{Units: m.Units + other.Units, Currency: m.Currency}, nil
}

// Subtract subtrai valores da mesma moeda; o resultado interno pode ser negativo.
func (m Money) Subtract(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	const maxInt64 = int64(1<<63 - 1)
	const minInt64 = -1 << 63
	if (other.Units > 0 && m.Units < minInt64+other.Units) ||
		(other.Units < 0 && m.Units > maxInt64+other.Units) {
		return Money{}, ErrOverflow
	}
	return Money{Units: m.Units - other.Units, Currency: m.Currency}, nil
}

// Negate troca o sinal e rejeita o único valor que não cabe após a troca.
func (m Money) Negate() (Money, error) {
	if m.Units == -1<<63 {
		return Money{}, ErrOverflow
	}
	return Money{Units: -m.Units, Currency: m.Currency}, nil
}

// Compare compara valores da mesma moeda: -1 menor, 0 igual, 1 maior.
func (m Money) Compare(other Money) (int, error) {
	if m.Currency != other.Currency {
		return 0, ErrCurrencyMismatch
	}
	if m.Units < other.Units {
		return -1, nil
	}
	if m.Units > other.Units {
		return 1, nil
	}
	return 0, nil
}

// String mostra o valor com duas casas decimais e o código da moeda.
func (m Money) String() string {
	negative := m.Units < 0
	var magnitude uint64
	if negative {
		// Esta forma também funciona para o menor int64, cujo positivo não cabe.
		magnitude = uint64(-(m.Units + 1)) + 1
	} else {
		magnitude = uint64(m.Units)
	}
	amount := strconv.FormatUint(magnitude/100, 10) + "."
	cents := magnitude % 100
	if cents < 10 {
		amount += "0"
	}
	amount += strconv.FormatUint(cents, 10)
	if negative {
		amount = "-" + amount
	}
	return amount + " " + m.Currency
}
