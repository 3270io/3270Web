package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jnnngs/3270Web/internal/apitoken"
	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/users"
)

const tokenUsage = `Manage API tokens for automated clients.

Usage:
  3270Web token add <username> <name> [--read-only] [--expires <duration>]
                                          Issue a token for an account
  3270Web token list [username]           List tokens
  3270Web token revoke <id>               Stop a token working
  3270Web token revoke-all <username>     Stop all of an account's tokens

A token reaches exactly what its owner reaches: their sessions, their saved
work, and nothing belonging to anybody else. --read-only issues one that can
read but not type, press keys, or open sessions.

The token is shown once, when it is issued. It is stored as a hash, so a lost
token is replaced rather than recovered.

Tokens apply when AUTH_MODE=local. Without accounts there is one operator and
one API_TOKEN environment variable, which is all there is to say about who is
calling.
`

// runTokenCLI implements the `token` subcommand.
func runTokenCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, tokenUsage)
		return 2
	}

	tokens := apitoken.NewStore(resolveTokensPath())
	accounts := users.NewStore(resolveUsersPath())
	// The CLI writes to the same trail the server does. Issuing a credential
	// from a container shell is exactly the act somebody would want a record
	// of, and it leaves no other trace.
	trail := audit.NewRecorder(resolveAuditPath())

	switch args[0] {
	case "add", "create":
		return tokenAdd(tokens, accounts, trail, args[1:], stdout, stderr)
	case "list", "ls":
		return tokenList(tokens, accounts, args[1:], stdout, stderr)
	case "revoke":
		return tokenRevoke(tokens, trail, args[1:], stdout, stderr)
	case "revoke-all":
		return tokenRevokeAll(tokens, accounts, trail, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, tokenUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown token command %q\n\n%s", args[0], tokenUsage)
		return 2
	}
}

func tokenAdd(tokens *apitoken.Store, accounts *users.Store, trail *audit.Recorder, args []string, stdout, stderr io.Writer) int {
	var positional []string
	scopes := []string{apitoken.ScopeRead, apitoken.ScopeWrite}
	var expiresAt *time.Time

	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--read-only":
			scopes = []string{apitoken.ScopeRead}
		case "--expires":
			if i+1 >= len(args) {
				fmt.Fprint(stderr, "--expires needs a duration, for example 720h\n")
				return 2
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil || d <= 0 {
				fmt.Fprintf(stderr, "cannot read %q as a duration; try 24h or 720h\n", args[i])
				return 2
			}
			when := time.Now().Add(d)
			expiresAt = &when
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(stderr, "unknown option %q\n", arg)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 2 {
		fmt.Fprint(stderr, "a username and a name for the token are required\n\n"+tokenUsage)
		return 2
	}
	username, name := positional[0], positional[1]

	owner, ok := findAccount(accounts, username, stderr)
	if !ok {
		return 1
	}

	record, secret, err := tokens.Issue(owner.ID, name, scopes, expiresAt)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	trail.Log(audit.Entry{
		Event:  audit.EventTokenIssued,
		Actor:  audit.Actor{Username: "console"},
		Target: record.ID,
		Detail: map[string]string{
			"account": owner.Username,
			"name":    record.Name,
			"scopes":  strings.Join(record.Scopes, "+"),
		},
	})

	fmt.Fprintf(stdout, "issued %s for %s (%s)\n", record.ID, owner.Username, strings.Join(record.Scopes, "+"))
	if record.ExpiresAt != nil {
		fmt.Fprintf(stdout, "expires %s\n", record.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintf(stdout, "\n  %s\n\n", secret)
	fmt.Fprintln(stdout, "This is the only time the token is shown. Store it somewhere the client can read")
	fmt.Fprintln(stdout, "and nobody else can.")
	return 0
}

func tokenList(tokens *apitoken.Store, accounts *users.Store, args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprint(stderr, "at most one username\n\n"+tokenUsage)
		return 2
	}

	filterID := ""
	if len(args) == 1 {
		owner, ok := findAccount(accounts, args[0], stderr)
		if !ok {
			return 1
		}
		filterID = owner.ID
	}

	list, err := tokens.List()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	names := accountNames(accounts)
	rows := make([]apitoken.Token, 0, len(list))
	for _, tok := range list {
		if filterID == "" || tok.UserID == filterID {
			rows = append(rows, tok)
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "no tokens issued")
		return 0
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })

	now := time.Now()
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tACCOUNT\tNAME\tSCOPES\tSTATUS\tLAST USED")
	for _, tok := range rows {
		owner := names[tok.UserID]
		if owner == "" {
			// The account was deleted; the token no longer authenticates, but
			// saying so is more use than printing a bare ID.
			owner = "(deleted)"
		}
		lastUsed := "never"
		if tok.LastUsedAt != nil {
			lastUsed = tok.LastUsedAt.Format("2006-01-02")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			tok.ID, owner, tok.Name, strings.Join(tok.Scopes, "+"), tokenStatus(tok, now), lastUsed)
	}
	w.Flush()
	return 0
}

func tokenStatus(tok apitoken.Token, now time.Time) string {
	switch err := tok.Active(now); {
	case errors.Is(err, apitoken.ErrRevoked):
		return "revoked"
	case errors.Is(err, apitoken.ErrExpired):
		return "expired"
	case tok.ExpiresAt != nil:
		return "expires " + tok.ExpiresAt.Format("2006-01-02")
	default:
		return "active"
	}
}

func tokenRevoke(tokens *apitoken.Store, trail *audit.Recorder, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a token id is required\n\n"+tokenUsage)
		return 2
	}
	if err := tokens.Revoke(args[0]); err != nil {
		if errors.Is(err, apitoken.ErrNotFound) {
			fmt.Fprintf(stderr, "no token with id %q\n", args[0])
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	trail.Log(audit.Entry{
		Event: audit.EventTokenRevoked, Actor: audit.Actor{Username: "console"}, Target: args[0],
	})
	fmt.Fprintf(stdout, "revoked %s\n", args[0])
	return 0
}

func tokenRevokeAll(tokens *apitoken.Store, accounts *users.Store, trail *audit.Recorder, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a username is required\n\n"+tokenUsage)
		return 2
	}
	owner, ok := findAccount(accounts, args[0], stderr)
	if !ok {
		return 1
	}
	n, err := tokens.RevokeAllFor(owner.ID)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if n > 0 {
		trail.Log(audit.Entry{
			Event: audit.EventTokenRevoked, Actor: audit.Actor{Username: "console"},
			Target: owner.Username, Detail: map[string]string{"count": strconv.Itoa(n)},
		})
	}
	fmt.Fprintf(stdout, "revoked %d token(s) for %s\n", n, owner.Username)
	return 0
}

// findAccount resolves a username, reporting the failure itself so each
// caller does not repeat the message.
func findAccount(accounts *users.Store, username string, stderr io.Writer) (users.User, bool) {
	list, err := accounts.List()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return users.User{}, false
	}
	for _, u := range list {
		if strings.EqualFold(u.Username, username) {
			return u, true
		}
	}
	fmt.Fprintf(stderr, "no user named %q\n", username)
	return users.User{}, false
}

// accountNames maps account IDs to usernames, so a token list reads in terms
// of people rather than identifiers.
func accountNames(accounts *users.Store) map[string]string {
	names := map[string]string{}
	list, err := accounts.List()
	if err != nil {
		return names
	}
	for _, u := range list {
		names[u.ID] = u.Username
	}
	return names
}
