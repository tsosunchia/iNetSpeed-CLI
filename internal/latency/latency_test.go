package latency

import (
	"math"
	"testing"
)

func TestComputeEmpty(t *testing.T) {
	s := Compute(nil)
	if s.N != 0 {
		t.Errorf("N = %d, want 0", s.N)
	}
}

func TestComputeSingle(t *testing.T) {
	s := Compute([]float64{42.0})
	if s.N != 1 {
		t.Errorf("N = %d", s.N)
	}
	if s.Min != 42 || s.Max != 42 || s.Median != 42 || s.Avg != 42 {
		t.Errorf("unexpected stats for single sample: %+v", s)
	}
	if s.Jitter != 0 {
		t.Errorf("Jitter = %f, want 0", s.Jitter)
	}
}

func TestComputeOdd(t *testing.T) {
	// 3 samples: median is middle value
	s := Compute([]float64{10, 30, 20})
	if s.N != 3 {
		t.Errorf("N = %d", s.N)
	}
	if s.Median != 20 {
		t.Errorf("Median = %f, want 20", s.Median)
	}
	if s.Min != 10 {
		t.Errorf("Min = %f, want 10", s.Min)
	}
	if s.Max != 30 {
		t.Errorf("Max = %f, want 30", s.Max)
	}
}

func TestComputeEven(t *testing.T) {
	// 4 samples: median is average of middle two
	s := Compute([]float64{10, 20, 30, 40})
	if s.Median != 25 {
		t.Errorf("Median = %f, want 25", s.Median)
	}
}

func TestComputeJitter(t *testing.T) {
	// sorted: [10,20,30] → diffs: 10,10 → jitter = 10
	s := Compute([]float64{30, 10, 20})
	if s.Jitter != 10 {
		t.Errorf("Jitter = %f, want 10", s.Jitter)
	}
}

func TestComputeAvg(t *testing.T) {
	s := Compute([]float64{10, 20, 30})
	want := 20.0
	if math.Abs(s.Avg-want) > 0.01 {
		t.Errorf("Avg = %f, want %f", s.Avg, want)
	}
}
