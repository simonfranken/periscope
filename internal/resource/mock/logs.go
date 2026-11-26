package mock

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand/v2"
	"strings"
	"time"

	"periscope/internal/resource"
)

// Logs makes up a log that fits the container: requests for web apps, the
// crash of a crashlooping run, a postgres killed for memory, a failed
// migration. A running container's log goes on with a new line every few
// seconds. Errors are worded as the API server words them.
func (p *Provider) Logs(ctx context.Context, r resource.LogRequest) (resource.LogStream, error) {
	p.mu.Lock()
	st, ok := p.statuses[r.Pod.Path]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("pods %q not found", r.Pod.Name)
	}
	var c *resource.Container
	for i := range st.Containers {
		if st.Containers[i].Name == r.Container {
			c = &st.Containers[i]
		}
	}
	switch {
	case c == nil:
		return nil, fmt.Errorf("container %s is not valid for pod %s", r.Container, r.Pod.Name)
	case r.Previous && c.Restarts == 0:
		return nil, fmt.Errorf("previous terminated container %q in pod %q not found", c.Name, r.Pod.Name)
	case !r.Previous && c.State == resource.ContainerWaiting:
		return nil, fmt.Errorf("container %q in pod %q is waiting to start: %s", c.Name, r.Pod.Name, c.Reason)
	}
	h := fnv.New64a()
	h.Write([]byte(r.Pod.Path + "/" + c.Name))
	rnd := rand.New(rand.NewPCG(h.Sum64(), 1))
	g := &generator{rnd: rnd, image: c.Image, container: c.Name}
	now := time.Now()
	var lines []resource.LogLine
	switch {
	case r.Previous:
		end := c.LastExit
		if end.IsZero() {
			end = now.Add(-time.Minute)
		}
		lines = g.history(end, 60)
		lines = append(lines, g.ending(c.LastReason, end)...)
	case c.State == resource.ContainerTerminated:
		lines = g.history(now.Add(-2*time.Hour), 30)
		lines = append(lines, g.ending(c.Reason, now.Add(-2*time.Hour))...)
	default:
		start := now.Add(-time.Since(st.Created))
		if c.Restarts > 0 && !c.LastExit.IsZero() {
			start = c.LastExit
		}
		lines = g.startup(start)
		for _, l := range g.history(now, 150) {
			if l.Time.After(start) {
				lines = append(lines, l)
			}
		}
	}
	if r.Tail > 0 && len(lines) > r.Tail {
		lines = lines[len(lines)-r.Tail:]
	}
	live := !r.Previous && c.State == resource.ContainerRunning
	return &stream{ctx: ctx, lines: lines, live: live, g: g, first: true}, nil
}

type stream struct {
	ctx   context.Context
	lines []resource.LogLine
	live  bool
	g     *generator
	first bool
}

func (s *stream) Next() (resource.LogLine, error) {
	wait := time.Duration(0)
	switch {
	case s.first:
		wait = time.Duration(150+rand.IntN(300)) * time.Millisecond // like a request
	case len(s.lines) == 0 && !s.live:
		return resource.LogLine{}, io.EOF
	case len(s.lines) == 0:
		wait = time.Duration(800+rand.IntN(2500)) * time.Millisecond
	}
	s.first = false
	if wait > 0 {
		select {
		case <-s.ctx.Done():
			return resource.LogLine{}, s.ctx.Err()
		case <-time.After(wait):
		}
	}
	if len(s.lines) == 0 {
		if !s.live {
			return resource.LogLine{}, io.EOF
		}
		return resource.LogLine{Time: time.Now(), Text: s.g.line()}, nil
	}
	l := s.lines[0]
	s.lines = s.lines[1:]
	return l, nil
}

func (s *stream) Close() error { return nil }

// generator writes lines in the style of the container's image.
type generator struct {
	rnd              *rand.Rand
	image, container string
}

func (g *generator) is(s string) bool { return strings.Contains(g.image, s) || g.container == s }

// history is n lines up to end, a few seconds apart.
func (g *generator) history(end time.Time, n int) []resource.LogLine {
	out := make([]resource.LogLine, n)
	t := end
	for i := n - 1; i >= 0; i-- {
		t = t.Add(-time.Duration(500+g.rnd.IntN(6000)) * time.Millisecond)
		out[i] = resource.LogLine{Time: t, Text: g.line()}
	}
	return out
}

func (g *generator) startup(t time.Time) []resource.LogLine {
	var texts []string
	switch {
	case g.is("postgres"):
		texts = []string{
			"LOG:  starting PostgreSQL 17.2 on x86_64-pc-linux-gnu",
			"LOG:  listening on IPv4 address \"0.0.0.0\", port 5432",
			"LOG:  database system was interrupted; last known up at " + t.Add(-2*time.Second).UTC().Format("2006-01-02 15:04:05 UTC"),
			"LOG:  database system was not properly shut down; automatic recovery in progress",
			"LOG:  redo done at 0/3A1F2B8",
			"LOG:  database system is ready to accept connections",
		}
	case g.is("envoy"):
		texts = []string{"[info][main] initializing epoch 0 (base id=0, hot restart version=11.104)", "[info][config] all clusters initialized. initializing init manager", "[info][main] starting main dispatch loop"}
	case g.is("nginx"), g.is("web"):
		texts = []string{"/docker-entrypoint.sh: Configuration complete; ready for start up", "nginx/1.29.0 start worker processes"}
	default:
		texts = []string{`level=info msg="starting" version=` + version(g.image), `level=info msg="config loaded" source=/etc/app/config.yaml`, `level=info msg="listening" addr=:8080`}
	}
	out := make([]resource.LogLine, len(texts))
	for i, s := range texts {
		out[i] = resource.LogLine{Time: t.Add(time.Duration(i) * 40 * time.Millisecond), Text: s}
	}
	return out
}

func version(image string) string {
	if _, v, ok := strings.Cut(image, ":"); ok {
		return v
	}
	return "latest"
}

// ending is how a run that ended for reason finished.
func (g *generator) ending(reason string, t time.Time) []resource.LogLine {
	var texts []string
	switch {
	case reason == "OOMKilled" && g.is("postgres"):
		texts = []string{"LOG:  checkpoint starting: time", "LOG:  duration: 8211.493 ms  statement: SELECT * FROM orders o JOIN order_items i ON i.order_id = o.id ORDER BY o.created_at"}
	case reason == "OOMKilled":
		texts = []string{`level=info msg="loading cache" entries=1840000`}
	case g.is("migrate"):
		texts = []string{
			`level=info msg="applying migration" version=42 name=add_order_status`,
			`ERROR: relation "order_status" already exists (SQLSTATE 42P07)`,
			`level=error msg="migration failed" version=42 err="relation \"order_status\" already exists"`,
		}
	case g.is("grafana"):
		texts = []string{`logger=server level=error msg="Server shutdown" error="context canceled"`, `logger=sqlstore level=error msg="database is locked" retries=5`}
	default:
		texts = []string{
			`level=info msg="connecting to database" host=postgres:5432`,
			`level=warn msg="database not reachable, retrying" attempt=3`,
			`level=error msg="dial tcp 10.96.14.2:5432: connect: connection refused"`,
			`panic: database unreachable after 3 attempts`,
			``,
			`goroutine 1 [running]:`,
			`main.main()`,
			`	/src/cmd/api/main.go:58 +0x2c5`,
		}
	}
	out := make([]resource.LogLine, len(texts))
	for i, s := range texts {
		out[i] = resource.LogLine{Time: t.Add(time.Duration(i-len(texts)) * 10 * time.Millisecond), Text: s}
	}
	return out
}

var paths = []string{"/", "/api/v1/products", "/api/v1/cart", "/api/v1/orders", "/healthz", "/metrics", "/static/app.js"}

// line is one line of a running container's log.
func (g *generator) line() string {
	r := g.rnd
	switch {
	case g.is("postgres"):
		return fmt.Sprintf("LOG:  duration: %.3f ms  statement: SELECT id, status FROM orders WHERE id = %d", r.Float64()*20, 1000+r.IntN(9000))
	case g.is("envoy"):
		return fmt.Sprintf(`[%s] "GET %s HTTP/1.1" %d - 0 %d %d "-" "kube-probe/1.34"`, "info][upstream", paths[r.IntN(len(paths))], 200, r.IntN(5000), r.IntN(30))
	case g.is("fraud"):
		if r.IntN(3) == 0 {
			return `level=warn msg="readiness check failed" reason="model not loaded" elapsed=` + fmt.Sprintf("%ds", 30+r.IntN(300))
		}
		return fmt.Sprintf(`level=info msg="scored transaction" id=tx_%06d score=%.2f`, r.IntN(1000000), r.Float64())
	case g.is("coredns"):
		return fmt.Sprintf(`[INFO] 10.244.%d.%d:%d - %d "A IN api.shop.svc.cluster.local. udp 52 false 512" NOERROR qr,aa,rd 98 0.000%dms`, r.IntN(3), 2+r.IntN(200), 30000+r.IntN(30000), r.IntN(65535), 100+r.IntN(900))
	case g.is("kube-proxy"):
		return fmt.Sprintf(`I1125 %s.%06d       1 proxier.go:%d] "Syncing iptables rules" ipFamily="IPv4"`, time.Now().Format("15:04:05"), r.IntN(1000000), 800+r.IntN(100))
	case g.is("node-exporter"):
		return fmt.Sprintf(`ts=%s caller=collector.go:%d level=debug msg="collector succeeded" name=%s duration_seconds=0.00%d`, time.Now().UTC().Format(time.RFC3339), 160+r.IntN(20), []string{"cpu", "meminfo", "filesystem", "netdev"}[r.IntN(4)], r.IntN(9000))
	case g.is("nginx"), g.is("web"):
		return fmt.Sprintf(`10.244.%d.%d - - "GET %s HTTP/1.1" %d %d "-" "Mozilla/5.0"`, r.IntN(3), 2+r.IntN(200), paths[r.IntN(len(paths))], []int{200, 200, 200, 304, 404}[r.IntN(5)], 200+r.IntN(20000))
	}
	if r.IntN(25) == 0 {
		return fmt.Sprintf(`level=warn msg="slow request" path=%s duration=%dms`, paths[r.IntN(len(paths))], 900+r.IntN(2000))
	}
	return fmt.Sprintf(`level=info msg="request" method=GET path=%s status=200 duration=%dms`, paths[r.IntN(len(paths))], 1+r.IntN(40))
}
