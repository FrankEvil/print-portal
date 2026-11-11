package job

import (
	"context"
	"testing"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/layout"
)

// The laid-out PDF already carries the target paper geometry, so orientation
// and scaling must not be forwarded to CUPS a second time.
func TestLaidOutSkipsOrientationAndScaling(t *testing.T) {
	opts := cups.PrintOptions{
		Orientation: "landscape",
		Scaling:     "fill",
		LaidOut:     true,
	}
	args := cups.PrintOptionArgsForTest(opts)
	for _, arg := range args {
		if arg == "orientation-requested=4" {
			t.Fatalf("orientation must not be forwarded after layout: %v", args)
		}
		if arg == "fill" || arg == "fit-to-page" {
			t.Fatalf("scaling must not be forwarded after layout: %v", args)
		}
	}
}

// Without layout the same options still reach CUPS.
func TestNonLaidOutKeepsOrientationAndScaling(t *testing.T) {
	opts := cups.PrintOptions{
		Orientation: "landscape",
		Scaling:     "fill",
	}
	args := cups.PrintOptionArgsForTest(opts)
	var hasOrientation, hasScaling bool
	for _, arg := range args {
		if arg == "orientation-requested=4" {
			hasOrientation = true
		}
		if arg == "fill" {
			hasScaling = true
		}
	}
	if !hasOrientation || !hasScaling {
		t.Fatalf("expected orientation and scaling, got %v", args)
	}
}

// Layout must be active only when it actually changes the document.
func TestLayoutOptsActive(t *testing.T) {
	if layoutOptsActive(layout.Options{PagesPerSheet: 1}) {
		t.Fatal("single page per sheet without extras is not an active layout")
	}
	for _, opts := range []layout.Options{
		{PagesPerSheet: 2},
		{Border: true},
		{Reverse: true},
		{FlipHorizontal: true},
	} {
		if !layoutOptsActive(opts) {
			t.Fatalf("expected active layout for %+v", opts)
		}
	}
}

// Duplex jobs must be chunked in even numbers, otherwise every chunk starts a
// new front/back pairing and the pages shift after the first chunk.
func TestEffectiveChunkPages(t *testing.T) {
	cases := []struct {
		base   int
		duplex string
		want   int
	}{
		{3, "two-sided-long-edge", 4},
		{3, "two-sided-short-edge", 4},
		{1, "two-sided-long-edge", 2},
		{3, "one-sided", 3},
		{4, "two-sided-long-edge", 4},
		{0, "two-sided-long-edge", 0},
	}
	for _, tc := range cases {
		if got := effectiveChunkPages(tc.base, tc.duplex); got != tc.want {
			t.Errorf("effectiveChunkPages(%d, %q) = %d, want %d", tc.base, tc.duplex, got, tc.want)
		}
	}
}

var _ = context.Background
