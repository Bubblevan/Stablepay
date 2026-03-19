package resilience

import (
	"context"
	"errors"
	"time"

	"github.com/sony/gobreaker"
)

type Runner struct {
	breakers map[string]*gobreaker.CircuitBreaker
}

func NewRunner(targets []string) *Runner {
	breakers := make(map[string]*gobreaker.CircuitBreaker, len(targets))
	for _, target := range targets {
		breakers[target] = gobreaker.NewCircuitBreaker(gobreaker.Settings{Name: target})
	}
	return &Runner{breakers: breakers}
}

func (r *Runner) Do(ctx context.Context, target string, timeout time.Duration, retry int, fn func(context.Context) error) error {
	breaker, ok := r.breakers[target]
	if !ok {
		return fn(ctx)
	}
	var lastErr error
	for i := 0; i <= retry; i++ {
		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		_, err := breaker.Execute(func() (interface{}, error) {
			return nil, fn(timeoutCtx)
		})
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		return err
	}
	return lastErr
}
