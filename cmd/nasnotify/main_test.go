package main

import (
	"testing"
	"time"
)

func TestDurationFromMinutesUsesFallback(t *testing.T) {
	got := durationFromMinutes(0, 60)
	if got != time.Hour {
		t.Fatalf("durationFromMinutes(0, 60) = %v; want %v", got, time.Hour)
	}
}

func TestDurationFromMinutesSupportsFractionalMinutes(t *testing.T) {
	got := durationFromMinutes(0.5, 60)
	want := 30 * time.Second
	if got != want {
		t.Fatalf("durationFromMinutes(0.5, 60) = %v; want %v", got, want)
	}
}
