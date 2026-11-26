package kube

import (
	"bufio"
	"context"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"periscope/internal/resource"
)

// Logs streams a container's log as kubectl logs --timestamps does, and
// follows it unless it is a previous run's.
func (p *Provider) Logs(ctx context.Context, r resource.LogRequest) (resource.LogStream, error) {
	opts := &corev1.PodLogOptions{Container: r.Container, Previous: r.Previous, Follow: !r.Previous, Timestamps: true}
	if r.Tail > 0 {
		tail := int64(r.Tail)
		opts.TailLines = &tail
	}
	rc, err := p.logs.CoreV1().Pods(r.Pod.Namespace).GetLogs(r.Pod.Name, opts).Stream(ctx)
	if err != nil {
		return nil, err
	}
	return &logStream{rc: rc, br: bufio.NewReaderSize(rc, 64<<10)}, nil
}

type logStream struct {
	rc io.ReadCloser
	br *bufio.Reader
}

func (s *logStream) Next() (resource.LogLine, error) {
	line, err := s.br.ReadString('\n')
	if line == "" && err != nil {
		return resource.LogLine{}, err
	}
	return parseLogLine(strings.TrimRight(line, "\r\n")), nil
}

func (s *logStream) Close() error { return s.rc.Close() }

// parseLogLine splits the timestamp the kubelet puts in front of each line.
func parseLogLine(line string) resource.LogLine {
	ts, text, ok := strings.Cut(line, " ")
	if !ok {
		ts, text = line, ""
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return resource.LogLine{Text: line}
	}
	return resource.LogLine{Time: t, Text: text}
}
