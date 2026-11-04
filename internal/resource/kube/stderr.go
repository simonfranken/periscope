package kube

import (
	"flag"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// Quiet keeps client-go from writing to the terminal, which belongs to the
// UI: klog and API warnings are dropped, and what an exec credential plugin
// writes to stderr (a login URL, why it failed) is kept, so a failing Tree
// can say it. Call it before New: a plugin's stderr is fixed when its
// client is made. restore puts stderr back.
func Quiet() (restore func()) {
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	fs.Set("logtostderr", "false")
	fs.Set("alsologtostderr", "false")
	fs.Set("stderrthreshold", "FATAL")
	klog.SetOutput(io.Discard)
	rest.SetDefaultWarningHandler(rest.NoWarnings{})

	r, w, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	orig := os.Stderr
	os.Stderr = w
	go captured.read(r)
	return func() { os.Stderr = orig }
}

// captured is what was written to stderr since Quiet.
var captured tail

// tail keeps the last bytes written to stderr and counts all of them, so a
// caller can ask for what was written since a mark.
type tail struct {
	mu    sync.Mutex
	buf   []byte
	total int
}

const tailSize = 4096

func (t *tail) read(r io.Reader) {
	b := make([]byte, 1024)
	for {
		n, err := r.Read(b)
		t.mu.Lock()
		t.buf = append(t.buf, b[:n]...)
		if len(t.buf) > tailSize {
			t.buf = t.buf[len(t.buf)-tailSize:]
		}
		t.total += n
		t.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func (t *tail) mark() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}

// since returns what was written after mark, its lines joined, at most a
// few hundred bytes of the end.
func (t *tail) since(mark int) string {
	t.mu.Lock()
	n := min(t.total-mark, len(t.buf))
	s := string(t.buf[len(t.buf)-n:])
	t.mu.Unlock()
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !slices.Contains(lines, l) {
			lines = append(lines, l)
		}
	}
	s = strings.Join(lines, " ")
	if len(s) > 400 {
		s = "…" + s[len(s)-400:]
	}
	return s
}
