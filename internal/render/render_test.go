package render

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPlainRendererAllKinds(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf)

	events := []Event{
		{Kind: KindBanner, Value: "Test Banner"},
		{Kind: KindHeader, Value: "Test Header"},
		{Kind: KindInfo, Value: "info msg"},
		{Kind: KindWarn, Value: "warn msg"},
		{Kind: KindResult, Value: "result msg"},
		{Kind: KindKV, Label: "Key", Value: "Value"},
		{Kind: KindLine},
		{Kind: KindProgress, Label: "DL", Value: "50 Mbps"},
		{Kind: KindFatal, Value: "fatal msg"},
	}

	for _, ev := range events {
		r.Render(ev)
	}

	out := buf.String()
	checks := []string{"Test Banner", "> Test Header", "[+] info msg",
		"[!] warn msg", "-> result msg", "Key:", "Value",
		"----", "[DL] 50 Mbps", "[X] fatal msg"}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("output missing %q", c)
		}
	}
}

func TestTTYRendererProgressOverwrite(t *testing.T) {
	var buf bytes.Buffer
	r := &TTYRenderer{w: &buf}

	r.Render(Event{Kind: KindProgress, Label: "DL", Value: "10 Mbps"})
	r.Render(Event{Kind: KindProgress, Label: "DL", Value: "20 Mbps"})
	r.Render(Event{Kind: KindInfo, Value: "done"})

	out := buf.String()
	if !strings.Contains(out, "20 Mbps") {
		t.Error("missing progress update")
	}
	if !strings.Contains(out, "done") {
		t.Error("missing info after progress")
	}
}

func TestBusConcurrent(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf)
	bus := NewBus(r)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			bus.Info("msg")
		}
		close(done)
	}()
	<-done
	bus.Close()

	lines := strings.Count(buf.String(), "[+]")
	if lines != 100 {
		t.Errorf("expected 100 info lines, got %d", lines)
	}
}

func TestBusEventTimestamp(t *testing.T) {
	var lastEv Event
	r := &capRenderer{fn: func(ev Event) { lastEv = ev }}
	bus := NewBus(r)
	bus.Info("test")
	bus.Close()
	if lastEv.Time.IsZero() {
		t.Error("event time not set")
	}
	if time.Since(lastEv.Time) > time.Second {
		t.Error("event time too old")
	}
}

type capRenderer struct {
	fn func(Event)
}

func (c *capRenderer) Render(ev Event) { c.fn(ev) }
