package postgres

import "github.com/johnvictor01/challenge-jungle-gaming/internal/domain"

func testMoney(units int64, currency string) domain.Money {
	money, err := domain.NewMoney(units, currency)
	if err != nil {
		panic(err)
	}
	return money
}
