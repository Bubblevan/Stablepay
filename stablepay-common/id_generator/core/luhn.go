package core

import (
	"strconv"
)

// LuhnCalculate calculates the check digit for a given string of digits.
func LuhnCalculate(number string) (int, error) {
	sum := 0
	isSecond := true // Luhn typically processes from right to left, so if we append, we think about the position.
	// Standard Luhn: double every second digit from the right.
	// Since we are calculating the check digit which will be appended at the end,
	// the check digit itself will be at position 0 (from right), so the last digit of 'number' is at position 1.
	// Thus we double every digit at odd positions from the right (1, 3, 5...).
	// Wait, let's stick to the standard algorithm implementation.

	// We want to find x such that (number + x) is valid.
	// A number is valid if sum of digits (doubling every second from right) % 10 == 0.

	// Let's iterate from right to left of 'number'.
	// The rightmost digit of 'number' will be at position 2 (if we consider check digit is pos 1).
	// Actually, let's use a simpler approach:
	// 1. Calculate sum of 'number' processing from right to left, treating the first digit (rightmost) as being at position 2 (since check digit is at 1).
	// 2. The check digit is (10 - sum%10) % 10.

	n := len(number)
	for i := n - 1; i >= 0; i-- {
		d, err := strconv.Atoi(string(number[i]))
		if err != nil {
			return 0, err
		}

		if isSecond {
			d = d * 2
			if d > 9 {
				d = d - 9
			}
		}
		sum += d
		isSecond = !isSecond
	}

	return (10 - sum%10) % 10, nil
}

// LuhnValidate validates if the number string (including check digit) satisfies Luhn algorithm.
func LuhnValidate(number string) bool {
	sum := 0
	isSecond := false
	n := len(number)

	for i := n - 1; i >= 0; i-- {
		d, err := strconv.Atoi(string(number[i]))
		if err != nil {
			return false
		}

		if isSecond {
			d = d * 2
			if d > 9 {
				d = d - 9
			}
		}
		sum += d
		isSecond = !isSecond
	}

	return sum%10 == 0
}
