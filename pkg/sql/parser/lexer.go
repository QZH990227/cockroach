// Copyright 2018 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package parser

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/cockroachdb/cockroach/pkg/sql/coltypes"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
)

type lexer struct {
	in string
	// tokens contains tokens generated by the scanner.
	tokens []sqlSymType

	// The type that should be used when an INT is encountered.
	nakedIntType *coltypes.TInt
	// The type that should be used when a SERIAL is encountered.
	nakedSerialType *coltypes.TSerial

	// lastPos is the position into the tokens slice of the last
	// token returned by Lex().
	lastPos int

	stmts     []tree.Statement
	lastError *parseErr
}

func (l *lexer) init(
	sql string, tokens []sqlSymType, nakedIntType *coltypes.TInt, nakedSerialType *coltypes.TSerial,
) {
	l.in = sql
	l.tokens = tokens
	l.lastPos = -1
	l.stmts = nil
	l.lastError = nil

	l.nakedIntType = nakedIntType
	l.nakedSerialType = nakedSerialType
}

// Lex lexes a token from input.
func (l *lexer) Lex(lval *sqlSymType) int {
	l.lastPos++
	// The core lexing takes place in the scanner. Here we do a small bit of post
	// processing of the lexical tokens so that the grammar only requires
	// one-token lookahead despite SQL requiring multi-token lookahead in some
	// cases. These special cases are handled below and the returned tokens are
	// adjusted to reflect the lookahead (LA) that occurred.
	if l.lastPos >= len(l.tokens) {
		lval.id = 0
		lval.pos = len(l.in)
		lval.str = "EOF"
		return 0
	}
	*lval = l.tokens[l.lastPos]

	switch lval.id {
	case NOT, WITH, AS:
		nextID := 0
		if l.lastPos+1 < len(l.tokens) {
			nextID = l.tokens[l.lastPos+1].id
		}

		// If you update these cases, update lex.lookaheadKeywords.
		switch lval.id {
		case AS:
			switch nextID {
			case OF:
				lval.id = AS_LA
			}
		case NOT:
			switch nextID {
			case BETWEEN, IN, LIKE, ILIKE, SIMILAR:
				lval.id = NOT_LA
			}

		case WITH:
			switch nextID {
			case TIME, ORDINALITY:
				lval.id = WITH_LA
			}
		}
	}

	return lval.id
}

func (l *lexer) lastToken() sqlSymType {
	if l.lastPos < 0 {
		return sqlSymType{}
	}

	if l.lastPos >= len(l.tokens) {
		return sqlSymType{
			id:  0,
			pos: len(l.in),
			str: "EOF",
		}
	}
	return l.tokens[l.lastPos]
}

// parseErr holds parsing error state.
type parseErr struct {
	msg                  string
	hint                 string
	detail               string
	unimplementedFeature string
}

func (l *lexer) initLastErr() {
	if l.lastError == nil {
		l.lastError = new(parseErr)
	}
}

// Unimplemented wraps Error, setting lastUnimplementedError.
func (l *lexer) Unimplemented(feature string) {
	l.Error("unimplemented")
	l.lastError.unimplementedFeature = feature
}

// UnimplementedWithIssue wraps Error, setting lastUnimplementedError.
func (l *lexer) UnimplementedWithIssue(issue int) {
	l.Error("unimplemented")
	l.lastError.unimplementedFeature = fmt.Sprintf("#%d", issue)
	l.lastError.hint = fmt.Sprintf("See: https://github.com/cockroachdb/cockroach/issues/%d", issue)
}

// UnimplementedWithIssueDetail wraps Error, setting lastUnimplementedError.
func (l *lexer) UnimplementedWithIssueDetail(issue int, detail string) {
	l.Error("unimplemented")
	l.lastError.unimplementedFeature = fmt.Sprintf("#%d.%s", issue, detail)
	l.lastError.hint = fmt.Sprintf("See: https://github.com/cockroachdb/cockroach/issues/%d", issue)
}

func (l *lexer) Error(e string) {
	l.initLastErr()
	lastTok := l.lastToken()
	if lastTok.id == ERROR {
		// This is a tokenizer (lexical) error: just emit the invalid
		// input as error.
		l.lastError.msg = lastTok.str
	} else {
		// This is a contextual error. Print the provided error message
		// and the error context.
		l.lastError.msg = fmt.Sprintf("%s at or near \"%s\"", e, lastTok.str)
	}

	// Find the end of the line containing the last token.
	i := strings.IndexByte(l.in[lastTok.pos:], '\n')
	if i == -1 {
		i = len(l.in)
	} else {
		i += lastTok.pos
	}
	// Find the beginning of the line containing the last token. Note that
	// LastIndexByte returns -1 if '\n' could not be found.
	j := strings.LastIndexByte(l.in[:lastTok.pos], '\n') + 1
	// Output everything up to and including the line containing the last token.
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "source SQL:\n%s\n", l.in[:i])
	// Output a caret indicating where the last token starts.
	fmt.Fprintf(&buf, "%s^", strings.Repeat(" ", lastTok.pos-j))
	l.lastError.detail = buf.String()
	l.lastError.unimplementedFeature = ""
	l.lastError.hint = ""
}

// SetHelp marks the "last error" field in the lexer to become a
// help text. This method is invoked in the error action of the
// parser, so the help text is only produced if the last token
// encountered was HELPTOKEN -- other cases are just syntax errors,
// and in that case we do not want the help text to overwrite the
// lastError field, which was set earlier to contain details about the
// syntax error.
func (l *lexer) SetHelp(msg HelpMessage) {
	if lastTok := l.lastToken(); lastTok.id == HELPTOKEN {
		l.populateHelpMsg(msg.String())
	} else {
		l.initLastErr()
		if msg.Command != "" {
			l.lastError.hint = `try \h ` + msg.Command
		} else {
			l.lastError.hint = `try \hf ` + msg.Function
		}
	}
}

func (l *lexer) populateHelpMsg(msg string) {
	l.initLastErr()
	l.lastError.unimplementedFeature = ""
	l.lastError.msg = "help token in input"
	l.lastError.hint = msg
}
