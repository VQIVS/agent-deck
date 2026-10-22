package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, input := range []string{
		`{"sessions":[{"name":"A","session":"session-a","dir":"/tmp/project"}]}`,
		"master: {name: A, session: session-a, dir: /tmp/project}\nworkers: []",
		"sessions:\n - {name: A, session: session-a, dir: /tmp/project, role: lead}",
	} {
		d, e := Parse([]byte(input))
		if e != nil {
			t.Fatal(e)
		}
		if d.Server != "http://127.0.0.1:4096" || len(d.Agents()) != 1 {
			t.Fatalf("%+v", d)
		}
	}
	for _, input := range []string{
		"sessions: []", "unknown: true", "server: ftp://host\nsessions: []",
		"sessions: [{name: A, session: bad, dir: relative}]",
		"sessions: [{name: A, session: session-a, dir: /tmp, role: worker}]",
		"sessions: [{name: A, session: session-a, dir: /tmp}, {name: B, session: session-a, dir: /tmp}]",
	} {
		if _, e := Parse([]byte(input)); e == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestUnlimitedAndWrite(t *testing.T) {
	var b strings.Builder
	b.WriteString("sessions:\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, " - {name: A%d, session: session-%d, dir: /tmp/project}\n", i, i)
	}
	d, e := Parse([]byte(b.String()))
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Agents()) != 40 {
		t.Fatal("truncated")
	}
	p := t.TempDir() + "/deck.yaml"
	if e = Write(t.Context(), p, &d); e != nil {
		t.Fatal(e)
	}
	if e = Write(t.Context(), p, &d); e == nil {
		t.Fatal("overwrote file")
	}
	got, e := Load(t.Context(), p)
	if e != nil || len(got.Agents()) != 40 {
		t.Fatalf("%+v %v", got, e)
	}
}
