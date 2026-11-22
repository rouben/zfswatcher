//
// logger_smtp.go
//
// Copyright © 2012-2013 Damicon Kraa Oy
//
// This file is part of zfswatcher.
//
// Zfswatcher is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zfswatcher is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with zfswatcher. If not, see <http://www.gnu.org/licenses/>.
//

package notifier

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// smtpTimeout limits connecting to and talking with the mail server, so a
// hanging server cannot block the daemon (variable so tests can shorten it).
var smtpTimeout = 30 * time.Second

// buildEmailMessage assembles an RFC 5322 message. Header values containing
// CR or LF are rejected to prevent header injection.
func buildEmailMessage(from, to, subject, text string, now time.Time) ([]byte, error) {
	for _, v := range []string{from, to, subject} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, errors.New("line break in mail header value")
		}
	}
	domain, _ := os.Hostname()
	if i := strings.LastIndex(from, "@"); i >= 0 {
		domain = strings.Trim(from[i+1:], "<> ")
	}
	if domain == "" {
		domain = "localhost"
	}
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	msgID := fmt.Sprintf("<%d.%s@%s>", now.UnixNano(), hex.EncodeToString(rnd[:]), domain)

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")
	msg := "From: " + from + "\r\n" +
		"To: " + strings.Join(strings.Fields(to), ", ") + "\r\n" +
		"Date: " + now.Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: " + msgID + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" + text + "\r\n"
	return []byte(msg), nil
}

// sendMailSMTP delivers one message. STARTTLS is used when the server
// offers it; PLAIN authentication (if username is set) is only sent over
// TLS or to localhost, which net/smtp enforces.
func sendMailSMTP(server, username, password, from string, rcpt []string, msg []byte) error {
	host, _, err := net.SplitHostPort(server)
	if err != nil {
		return fmt.Errorf("error parsing mail host:port: %w", err)
	}
	conn, err := (&net.Dialer{Timeout: smtpTimeout}).Dial("tcp", server)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if err = c.Hello(localHostname()); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err = c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if username != "" {
		if err = c.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return err
		}
	}
	envFrom := from
	if a, perr := mail.ParseAddress(from); perr == nil {
		envFrom = a.Address
	}
	if err = c.Mail(envFrom); err != nil {
		return err
	}
	for _, r := range rcpt {
		if err = c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(msg); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func localHostname() string {
	if h, err := os.Hostname(); err == nil && h != "" && !strings.ContainsAny(h, " \t") {
		return h
	}
	return "localhost"
}

func (n *Notifier) sendEmailSMTP(server, username, password, from, to, subject, text string) {
	defer n.wg.Done()

	msg, err := buildEmailMessage(from, to, subject, text, time.Now())
	if err != nil {
		checkInternalError("error building mail (giving up)", err)
		return
	}
	for retries := 0; true; retries++ {
		err := sendMailSMTP(server, username, password, from, strings.Fields(to), msg)
		if err == nil {
			break
		}
		if retries < 3 {
			checkInternalError("error sending mail (retrying)", err)
			time.Sleep(retry_SLEEP * time.Millisecond)
		} else {
			checkInternalError("error sending mail (giving up)", err)
			break
		}
	}
}

func makeEmailText(mbuf, abuf []string) (text string) {
	if mbuf != nil {
		text += strings.Join(mbuf, "\n")
		text += "\n"
	}
	if abuf != nil {
		text += strings.Join(abuf, "\n")
		text += "\n"
	}
	return text
}

func (n *Notifier) loggerEmailSMTP(ch chan *Msg, server, username, password, from, to, subject string, throttle time.Duration) {
	defer n.wg.Done()
	var mbuf []string
	var abuf []string
	var lastFlush time.Time
	var throttleTimer *time.Timer

	severityTrack := DEBUG
	hostname, _ := os.Hostname()

	for {
		var throttleC <-chan time.Time

		if throttleTimer != nil {
			throttleC = throttleTimer.C
		}
		var m *Msg
		var ok bool

		select {
		case m, ok = <-ch:
			// nothing
		case <-throttleC:
			m = &Msg{MsgType: MSGTYPE_FLUSH}
			ok = true
		}
		if !ok {
			break
		}
		switch m.MsgType {
		case MSGTYPE_MESSAGE:
			mbuf = append(mbuf, m.TimeString())
			if m.Severity < severityTrack {
				// keep track of worst severity within a batch of messages
				severityTrack = m.Severity
			}
		case MSGTYPE_ATTACHMENT:
			abuf = append(abuf, ">"+
				strings.ReplaceAll(strings.TrimRight(m.Text, "\n"), "\n", "\n>")+"\n")
		case MSGTYPE_FLUSH:
			if len(mbuf) == 0 && len(abuf) == 0 {
				continue
			}
			text := makeEmailText(mbuf, abuf)
			if text == "" {
				continue
			}
			if throttle != 0 && time.Since(lastFlush) < throttle {
				if throttleTimer == nil {
					throttleTimer = time.NewTimer(throttle - time.Since(lastFlush))
				}
				continue
			}
			mbuf, abuf = nil, nil
			sevsubject := subject + " [" + hostname + "] [" + severityTrack.String() + "]"
			severityTrack = DEBUG
			lastFlush = time.Now()
			if throttleTimer != nil {
				throttleTimer.Stop()
				throttleTimer = nil
			}
			n.wg.Add(1)
			go n.sendEmailSMTP(server, username, password, from, to, sevsubject, text)
		}
	}
	// exiting
	if throttleTimer != nil {
		throttleTimer.Stop()
	}
	// send the last entries:
	if text := makeEmailText(mbuf, abuf); text != "" {
		sevsubject := subject + " [" + hostname + "] [" + severityTrack.String() + "]"
		n.wg.Add(1)
		go n.sendEmailSMTP(server, username, password, from, to, sevsubject, text)
	}
}

// AddLoggerEmailSMTP adds an e-mail logging output. The e-mails are sent
// with ESMTP/SMTP whenever Flush() is called.
func (n *Notifier) AddLoggerEmailSMTP(s Severity, server, user, pass, from, to, subject string, throttle time.Duration) error {
	switch {
	case s < severity_MIN || s > severity_MAX:
		return errors.New(`invalid "severity"`)
	case server == "":
		return errors.New(`"server" not defined`)
	case from == "":
		return errors.New(`"from" not defined`)
	case to == "":
		return errors.New(`"to" not defined`)
	case subject == "":
		return errors.New(`"subject" not defined`)
	}
	ch := make(chan *Msg, chan_SIZE)
	n.wg.Add(1)
	go n.loggerEmailSMTP(ch, server, user, pass, from, to, subject, throttle)
	n.out = append(n.out, notifyOutput{severity: s, ch: ch, attachment: true, flush: true})
	return nil
}

// eof
