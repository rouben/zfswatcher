package notifier

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP accepts one connection and records the commands and the DATA body.
type fakeSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	cmds []string
	data string
	done chan struct{}
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, done: make(chan struct{})}
	t.Cleanup(func() { _ = ln.Close() })
	go f.serve()
	return f
}

func (f *fakeSMTP) serve() {
	defer close(f.done)
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.mu.Lock()
		f.cmds = append(f.cmds, line)
		f.mu.Unlock()
		switch verb := strings.ToUpper(strings.Fields(line + " x")[0]); verb {
		case "EHLO":
			w("250-fake")
			w("250 8BITMIME")
		case "DATA":
			w("354 go ahead")
			var sb strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				sb.WriteString(l)
			}
			f.mu.Lock()
			f.data = sb.String()
			f.mu.Unlock()
			w("250 queued")
		case "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func TestSendMailSMTP(t *testing.T) {
	f := newFakeSMTP(t)
	msg, err := buildEmailMessage("Zfs <zfs@example.org>", "a@example.org  b@example.org",
		"Pool ärger [host] [ALERT]", "line1\n.dot\nline3\n", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	err = sendMailSMTP(f.ln.Addr().String(), "", "", "Zfs <zfs@example.org>",
		[]string{"a@example.org", "b@example.org"}, msg)
	if err != nil {
		t.Fatal(err)
	}
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	all := strings.Join(f.cmds, "\n")
	for _, want := range []string{"EHLO ", "MAIL FROM:<zfs@example.org>",
		"RCPT TO:<a@example.org>", "RCPT TO:<b@example.org>", "DATA", "QUIT"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in conversation:\n%s", want, all)
		}
	}
	for _, want := range []string{"From: Zfs <zfs@example.org>\r\n",
		"To: a@example.org, b@example.org\r\n", "Date: ", "Message-ID: <",
		"@example.org>\r\n", "MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"Subject: =?utf-8?q?Pool_=C3=A4rger_[host]_[ALERT]?=\r\n",
		"\r\n\r\nline1\r\n..dot\r\nline3\r\n"} { // dot-stuffed on the wire
		if !strings.Contains(f.data, want) {
			t.Errorf("missing %q in data:\n%q", want, f.data)
		}
	}
}

func TestSendMailSMTPTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() { // accept but never answer
		c, err := ln.Accept()
		if err == nil {
			time.Sleep(5 * time.Second)
			_ = c.Close()
		}
	}()
	old := smtpTimeout
	smtpTimeout = 300 * time.Millisecond
	defer func() { smtpTimeout = old }()

	start := time.Now()
	err = sendMailSMTP(ln.Addr().String(), "", "", "a@example.org", []string{"b@example.org"}, []byte("x"))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took too long: %v", time.Since(start))
	}
}

func TestBuildEmailMessageInjection(t *testing.T) {
	cases := [][3]string{
		{"a@x\r\nBcc: evil@x", "b@x", "s"},
		{"a@x", "b@x\nBcc: evil@x", "s"},
		{"a@x", "b@x", "s\r\nBcc: evil@x"},
	}
	for _, c := range cases {
		if _, err := buildEmailMessage(c[0], c[1], c[2], "t", time.Now()); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}
