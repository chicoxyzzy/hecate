package governor

import "fmt"

// BudgetExceededError is returned by CheckRoute when the estimated request cost
// would exceed the current account balance.  Handlers map this to HTTP 402.
type BudgetExceededError struct {
	BalanceMicrosUSD int64
}

func (e *BudgetExceededError) Error() string {
	if e.BalanceMicrosUSD > 0 {
		return fmt.Sprintf("account balance insufficient (available: %d µUSD)", e.BalanceMicrosUSD)
	}
	return "account balance exhausted"
}
