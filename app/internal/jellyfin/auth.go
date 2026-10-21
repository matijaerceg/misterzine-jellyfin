package jellyfin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Sign-in replaces the plex.tv side (PIN link, server list, Home users):
// a Jellyfin client talks to one server at a known address, signs in with
// a user name and password or with Quick Connect, and gets a token. The
// server may list its users to pick from (PublicUsers).

// PublicInfo is the server's unauthenticated identity, the probe for an
// address the user typed.
type PublicInfo struct {
	ServerName string `json:"ServerName"`
	Version    string `json:"Version"`
	ID         string `json:"Id"`
	Product    string `json:"ProductName"`
	Address    string `json:"LocalAddress"`
	SetupDone  bool   `json:"StartupWizardCompleted"`
}

// PublicInfo probes the server (GET /System/Info/Public, no token needed).
// Anything that does not name itself a Jellyfin server is refused.
func (c *Client) PublicInfo(ctx context.Context) (*PublicInfo, error) {
	var info PublicInfo
	if err := c.getJSON(ctx, c.http, "/System/Info/Public", nil, &info); err != nil {
		return nil, err
	}
	if info.ID == "" || info.Version == "" {
		return nil, errors.New("not a Jellyfin server")
	}
	info.ServerName = Fold(info.ServerName)
	return &info, nil
}

type authResult struct {
	User struct {
		Name string `json:"Name"`
		ID   string `json:"Id"`
	} `json:"User"`
	AccessToken string `json:"AccessToken"`
	ServerID    string `json:"ServerId"`
}

func (c *Client) finishAuth(a *authResult) (Credentials, error) {
	if a.AccessToken == "" || a.User.ID == "" {
		return Credentials{}, errors.New("sign-in answer has no token")
	}
	cr := Credentials{UserID: a.User.ID, UserName: Fold(a.User.Name), Token: a.AccessToken, ServerID: a.ServerID}
	c.Store.SetCredentials(cr)
	return cr, nil
}

// SignIn signs in with a user name and password (POST
// /Users/AuthenticateByName) and keeps the session in the store. A wrong
// password is ErrUnauthorized.
func (c *Client) SignIn(ctx context.Context, user, password string) (Credentials, error) {
	c.Store.SetCredentials(Credentials{}) // a stale token must not ride along
	var a authResult
	err := c.post(ctx, "/Users/AuthenticateByName", nil, map[string]string{"Username": user, "Pw": password}, &a)
	if err != nil {
		return Credentials{}, err
	}
	return c.finishAuth(&a)
}

// PublicUser is a user the server lists on its sign-in screen. Name is as
// the server has it, for signing in; fold it to draw it.
type PublicUser struct {
	Name     string `json:"Name"`
	ID       string `json:"Id"`
	Password bool   `json:"HasPassword"`
	// Configured is the same answer under its other name; either one means
	// signing in needs the password.
	Configured bool   `json:"HasConfiguredPassword"`
	ImageTag   string `json:"PrimaryImageTag"`
}

// Protected reports whether signing in as u needs a password.
func (u PublicUser) Protected() bool { return u.Password || u.Configured }

// PublicUsers lists the users the server shows on its sign-in screen (GET
// /Users/Public, no token needed). Servers often hide every user, which is
// an empty list, not an error.
func (c *Client) PublicUsers(ctx context.Context) ([]PublicUser, error) {
	var users []PublicUser
	if err := c.getJSON(ctx, c.http, "/Users/Public", nil, &users); err != nil {
		return nil, err
	}
	out := users[:0]
	for _, u := range users {
		if u.ID != "" && strings.TrimSpace(u.Name) != "" {
			out = append(out, u)
		}
	}
	return out, nil
}

// QuickConnectEnabled reports whether the server offers Quick Connect.
func (c *Client) QuickConnectEnabled(ctx context.Context) (bool, error) {
	var on bool
	err := c.getJSON(ctx, c.http, "/QuickConnect/Enabled", nil, &on)
	return on, err
}

// QuickConnect is a pending Quick Connect request: the user enters Code in
// another signed-in client; Secret polls and redeems it.
type QuickConnect struct {
	Code          string `json:"Code"`
	Secret        string `json:"Secret"`
	Authenticated bool   `json:"Authenticated"`
}

// QuickConnectStart asks for a code (POST /QuickConnect/Initiate). A server
// with Quick Connect off answers 401.
func (c *Client) QuickConnectStart(ctx context.Context) (*QuickConnect, error) {
	var q QuickConnect
	if err := c.post(ctx, "/QuickConnect/Initiate", nil, nil, &q); err != nil {
		return nil, err
	}
	if q.Secret == "" || q.Code == "" {
		return nil, errors.New("quick connect answer has no code")
	}
	return &q, nil
}

// QuickConnectCheck reports whether the code has been approved (GET
// /QuickConnect/Connect?secret=). An expired request is an *HTTPError 404.
func (c *Client) QuickConnectCheck(ctx context.Context, secret string) (bool, error) {
	var q QuickConnect
	if err := c.getJSON(ctx, c.http, "/QuickConnect/Connect", url.Values{"secret": {secret}}, &q); err != nil {
		return false, err
	}
	return q.Authenticated, nil
}

// QuickConnectFinish redeems an approved request for a token (POST
// /Users/AuthenticateWithQuickConnect). Jellyfin 12 answers an unapproved
// or unknown secret with 404, not 401.
func (c *Client) QuickConnectFinish(ctx context.Context, secret string) (Credentials, error) {
	var a authResult
	if err := c.post(ctx, "/Users/AuthenticateWithQuickConnect", nil, map[string]string{"Secret": secret}, &a); err != nil {
		return Credentials{}, err
	}
	return c.finishAuth(&a)
}

// Me is the signed-in user (GET /Users/Me): the check that a stored token
// still works.
func (c *Client) Me(ctx context.Context) (Credentials, error) {
	var u struct {
		Name     string `json:"Name"`
		ID       string `json:"Id"`
		ServerID string `json:"ServerId"`
	}
	if err := c.getJSON(ctx, c.http, "/Users/Me", nil, &u); err != nil {
		return Credentials{}, err
	}
	cr := c.Store.Credentials()
	cr.UserID, cr.UserName = u.ID, Fold(u.Name)
	if u.ServerID != "" {
		cr.ServerID = u.ServerID
	}
	c.Store.SetCredentials(cr)
	return cr, nil
}

// SignOut ends the session on the server (POST /Sessions/Logout) and
// forgets the token whatever the server says.
func (c *Client) SignOut(ctx context.Context) error {
	if !c.SignedIn() {
		return nil
	}
	err := c.post(ctx, "/Sessions/Logout", nil, nil, nil)
	c.Store.SetCredentials(Credentials{})
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusUnauthorized {
		return nil // already gone
	}
	return err
}

// NormalizeHost turns what the user typed into a base URL: a scheme is
// added (https unless a port that is Jellyfin's plain HTTP one), the path
// kept for a server behind a reverse-proxy prefix, the trailing slash and
// any query dropped.
func NormalizeHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("no server address")
	}
	if !strings.Contains(s, "://") {
		if strings.HasSuffix(strings.SplitN(s, "/", 2)[0], ":8096") {
			s = "http://" + s
		} else {
			s = "https://" + s
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("not a server address")
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return strings.TrimRight(u.String(), "/"), nil
}

// HostCandidates is the base URLs to probe, in order, for what the user
// typed: NormalizeHost's, then for an address with neither scheme nor port
// also Jellyfin's plain HTTP port, so jellyfin.example.com reaches a server
// with no reverse proxy at http://jellyfin.example.com:8096.
func HostCandidates(s string) ([]string, error) {
	host, err := NormalizeHost(s)
	if err != nil {
		return nil, err
	}
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		return []string{host}, nil
	}
	u, err := url.Parse("http://" + s)
	if err != nil || u.Port() != "" || strings.HasSuffix(u.Host, ":") {
		return []string{host}, nil
	}
	u.Host += ":8096"
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return []string{host, strings.TrimRight(u.String(), "/")}, nil
}
