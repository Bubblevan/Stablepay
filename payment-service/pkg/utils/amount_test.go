// Package utils 金额工具单元测试
package utils

import (
	"testing"
)

func TestAmountUtil_ToMinorUnit(t *testing.T) {
	util := DefaultAmountUtil

	tests := []struct {
		name    string
		amount  string
		want    int64
		wantErr bool
	}{
		{
			name:   "正常金额 5.00 USDC",
			amount: "5.00",
			want:   5000000,
		},
		{
			name:   "整数金额 10 USDC",
			amount: "10",
			want:   10000000,
		},
		{
			name:   "小数金额 0.5 USDC",
			amount: "0.5",
			want:   500000,
		},
		{
			name:   "最小金额 0.000001 USDC",
			amount: "0.000001",
			want:   1,
		},
		{
			name:   "大金额 1000 USDC",
			amount: "1000",
			want:   1000000000,
		},
		{
			name:   "带空格金额",
			amount: " 5.00 ",
			want:   5000000,
		},
		{
			name:    "无效格式",
			amount:  "5.0.0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := util.ToMinorUnit(tt.amount)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToMinorUnit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ToMinorUnit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountUtil_FromMinorUnit(t *testing.T) {
	util := DefaultAmountUtil

	tests := []struct {
		name       string
		minorUnit  int64
		want       string
	}{
		{
			name:      "5000000 -> 5.000000",
			minorUnit: 5000000,
			want:      "5.000000",
		},
		{
			name:      "10000000 -> 10.000000",
			minorUnit: 10000000,
			want:      "10.000000",
		},
		{
			name:      "1 -> 0.000001",
			minorUnit: 1,
			want:      "0.000001",
		},
		{
			name:      "0 -> 0.000000",
			minorUnit: 0,
			want:      "0.000000",
		},
		{
			name:      "负数 -5000000 -> -5.000000",
			minorUnit: -5000000,
			want:      "-5.000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.FromMinorUnit(tt.minorUnit)
			if got != tt.want {
				t.Errorf("FromMinorUnit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountUtil_FromMinorUnitTrimZeros(t *testing.T) {
	util := DefaultAmountUtil

	tests := []struct {
		name      string
		minorUnit int64
		want      string
	}{
		{
			name:      "5000000 -> 5",
			minorUnit: 5000000,
			want:      "5",
		},
		{
			name:      "5500000 -> 5.5",
			minorUnit: 5500000,
			want:      "5.5",
		},
		{
			name:      "5000001 -> 5.000001",
			minorUnit: 5000001,
			want:      "5.000001",
		},
		{
			name:      "0 -> 0",
			minorUnit: 0,
			want:      "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.FromMinorUnitTrimZeros(tt.minorUnit)
			if got != tt.want {
				t.Errorf("FromMinorUnitTrimZeros() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAmountUtil_Compare(t *testing.T) {
	util := DefaultAmountUtil

	tests := []struct {
		name string
		a    int64
		b    int64
		want int
	}{
		{
			name: "5 > 3",
			a:    5000000,
			b:    3000000,
			want: 1,
		},
		{
			name: "3 < 5",
			a:    3000000,
			b:    5000000,
			want: -1,
		},
		{
			name: "5 = 5",
			a:    5000000,
			b:    5000000,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.Compare(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringToMinorUnit(t *testing.T) {
	// 测试简写函数
	got, err := StringToMinorUnit("5.00")
	if err != nil {
		t.Errorf("StringToMinorUnit() error = %v", err)
		return
	}
	if got != 5000000 {
		t.Errorf("StringToMinorUnit() = %v, want %v", got, 5000000)
	}
}

func TestMinorUnitToString(t *testing.T) {
	// 测试简写函数
	got := MinorUnitToString(5000000)
	if got != "5" {
		t.Errorf("MinorUnitToString() = %v, want %v", got, "5")
	}
}
