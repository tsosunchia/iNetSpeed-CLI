package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type EventKind int

const (
	KindBanner EventKind = iota
	KindHeader
	KindInfo
	KindWarn
	KindResult
	KindKV
	KindLine
	KindProgress
	KindFatal
)

type Event struct {
	Kind  EventKind
	Label string
	Value string
	Time  time.Time
}

type Bus struct {
	ch   chan Event
	wg   sync.WaitGroup
	once sync.Once
}

func NewBus(r Renderer) *Bus {
	b := &Bus{ch: make(chan Event, 256)}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for ev := range b.ch {
			r.Render(ev)
		}
	}()
	return b
}

func (b *Bus) Send(ev Event) {
	ev.Time = time.Now()
	b.ch <- ev
}

func (b *Bus) Close() {
	b.once.Do(func() { close(b.ch) })
	b.wg.Wait()
}

func (b *Bus) Banner(v string)          { b.Send(Event{Kind: KindBanner, Value: v}) }
func (b *Bus) Header(v string)          { b.Send(Event{Kind: KindHeader, Value: v}) }
func (b *Bus) Info(v string)            { b.Send(Event{Kind: KindInfo, Value: v}) }
func (b *Bus) Warn(v string)            { b.Send(Event{Kind: KindWarn, Value: v}) }
func (b *Bus) Result(v string)          { b.Send(Event{Kind: KindResult, Value: v}) }
func (b *Bus) KV(k, v string)           { b.Send(Event{Kind: KindKV, Label: k, Value: v}) }
func (b *Bus) Line()                    { b.Send(Event{Kind: KindLine}) }
func (b *Bus) Fatal(v string)           { b.Send(Event{Kind: KindFatal, Value: v}) }
func (b *Bus) Progress(label, v string) { b.Send(Event{Kind: KindProgress, Label: label, Value: v}) }

type Renderer interface {
	Render(Event)
}

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
	cRed    = "\033[31m"
)

type TTYRenderer struct {
	mu       sync.Mutex
	w        io.Writer
	lastProg string
}

func NewTTYRenderer() *TTYRenderer {
	return &TTYRenderer{w: os.Stderr}
}

func (t *TTYRenderer) Render(ev Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.lastProg != "" && ev.Kind != KindProgress {
		fmt.Fprintf(t.w, "\r%s\r", strings.Repeat(" ", len(t.lastProg)+2))
		t.lastProg = ""
	}

	switch ev.Kind {
	case KindBanner:
		fmt.Fprintf(t.w, "\n  %s%s%s%s\n", cCyan, cBold, ev.Value, cReset)
	case KindHeader:
		fmt.Fprintf(t.w, "\n%s%s  \u25b8 %s%s\n", cCyan, cBold, ev.Value, cReset)
	case KindInfo:
		fmt.Fprintf(t.w, "  %s%s[+]%s %s\n", cGreen, cBold, cReset, ev.Value)
	case KindWarn:
		fmt.Fprintf(t.w, "  %s%s[!]%s %s\n", cYellow, cBold, cReset, ev.Value)
	case KindResult:
		fmt.Fprintf(t.w, "  %s%s    \u279c  %s%s\n", cGreen, cBold, ev.Value, cReset)
	case KindKV:
		fmt.Fprintf(t.w, "  %s%s%-18s%s %s\n", cDim, cBold, ev.Label+":", cReset, ev.Value)
	case KindLine:
		fmt.Fprintf(t.w, "%s\n", cDim+"\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500"+cReset)
	case KindProgress:
		line := fmt.Sprintf("  %s[%s] %s%s", cDim, ev.Label, ev.Value, cReset)
		fmt.Fprintf(t.w, "\r%s", line)
		t.lastProg = line
	case KindFatal:
		fmt.Fprintf(t.w, "  %s%s[\u2717]%s %s\n", cRed, cBold, cReset, ev.Value)
	}
}

type PlainRenderer struct {
	mu sync.Mutex
	w  io.Writer
}

func NewPlainRenderer(w io.Writer) *PlainRenderer {
	return &PlainRenderer{w: w}
}

func (p *PlainRenderer) Render(ev Event) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch ev.Kind {
	case KindBanner:
		fmt.Fprintf(p.w, "\n  %s\n", ev.Value)
	case KindHeader:
		fmt.Fprintf(p.w, "\n  > %s\n", ev.Value)
	case KindInfo:
		fmt.Fprintf(p.w, "  [+] %s\n", ev.Value)
	case KindWarn:
		fmt.Fprintf(p.w, "  [!] %s\n", ev.Value)
	case KindResult:
		fmt.Fprintf(p.w, "      -> %s\n", ev.Value)
	case KindKV:
		fmt.Fprintf(p.w, "  %-18s %s\n", ev.Label+":", ev.Value)
	case KindLine:
		fmt.Fprintln(p.w, "  "+strings.Repeat("-", 56))
	case KindProgress:
		fmt.Fprintf(p.w, "  [%s] %s\n", ev.Label, ev.Value)
	case KindFatal:
		fmt.Fprintf(p.w, "  [X] %s\n", ev.Value)
	}
}

func IsTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
