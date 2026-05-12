package beanfun

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"
)

// Account is one game account under the user's Beanfun ID. The
// frontend uses these to render the post-login game list. See
// docs/beanfun-login-protocol.md § 8.
type Account struct {
	SID     string `json:"sid"`
	SSN     string `json:"ssn"`
	SName   string `json:"sname"`
	Enabled bool   `json:"enabled"`
}

// GetAccounts fetches the list of game accounts under the active
// session. Two sequential GETs:
//
//  1. auth.aspx — rebinds bfWebToken to a server-side game-zone
//     session. Response body discarded; non-2xx is fatal.
//  2. game_server_account_list.aspx — HTML page that lists accounts.
//     Parsed via accountRowRE; names HTML-entity decoded; sorted
//     ascending by SSN.
//
// Empty list is a valid response (user has no game accounts under
// this service code). Caller surfaces it to the UI as an empty state.
func (c *BeanfunClient) GetAccounts(ctx context.Context, session *Session) ([]Account, error) {
	if session == nil {
		return nil, ErrLoginRequired()
	}

	refreshURL, err := c.accountRefreshURL(session)
	if err != nil {
		return nil, err
	}
	req1, err := c.newRequest(ctx, "GET", refreshURL)
	if err != nil {
		return nil, err
	}
	resp1, err := c.http.Do(req1)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	if _, err := c.boundedRead(resp1); err != nil {
		return nil, err
	}
	if resp1.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("auth.aspx returned HTTP %d", resp1.StatusCode))
	}
	slog.Info("GetAccounts step 1: auth.aspx", "status", resp1.StatusCode)

	listURL, err := c.accountListURL(session)
	if err != nil {
		return nil, err
	}
	req2, err := c.newRequest(ctx, "GET", listURL)
	if err != nil {
		return nil, err
	}
	resp2, err := c.http.Do(req2)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	listBody, err := c.boundedRead(resp2)
	if err != nil {
		return nil, err
	}
	if resp2.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("game_server_account_list.aspx returned HTTP %d", resp2.StatusCode))
	}
	slog.Info("GetAccounts step 2: game_server_account_list.aspx",
		"status", resp2.StatusCode,
		"body_bytes", len(listBody))

	accounts := extractAccounts(string(listBody))
	slog.Info("GetAccounts: parsed", "count", len(accounts))
	return accounts, nil
}

// accountRefreshURL builds the auth.aspx URL with channel + page_and_query
// + web_token. The inner game_start.aspx?service_code_and_region=... is
// URL-encoded by net/url when serialised.
func (c *BeanfunClient) accountRefreshURL(session *Session) (*url.URL, error) {
	u, err := c.portalURL("beanfun_block/auth.aspx")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("channel", "game_zone")
	q.Set("page_and_query", fmt.Sprintf(
		"game_start.aspx?service_code_and_region=%s_%s",
		session.ServiceCode, session.ServiceRegion,
	))
	q.Set("web_token", session.WebToken)
	u.RawQuery = q.Encode()
	return u, nil
}

// accountListURL builds the game_server_account_list.aspx URL with
// sc/sr/dt query params. dt is "YYYYMMDDHHMMSS" UTC at call time.
func (c *BeanfunClient) accountListURL(session *Session) (*url.URL, error) {
	u, err := c.portalURL("beanfun_block/game_zone/game_server_account_list.aspx")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("sc", session.ServiceCode)
	q.Set("sr", session.ServiceRegion)
	q.Set("dt", time.Now().UTC().Format("20060102150405"))
	u.RawQuery = q.Encode()
	return u, nil
}
