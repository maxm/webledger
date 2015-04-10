package main

import (
	"code.google.com/p/goauth2/oauth"
	googleAuth "code.google.com/p/google-api-go-client/oauth2/v2"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type CookieData struct {
	Token oauth.Token
	Email string
}

var validCookies []CookieData = []CookieData{}

var oauthconfig = &oauth.Config{
	ClientId:     ClientId,
	ClientSecret: ClientSecret,
	Scope:        googleAuth.UserinfoEmailScope,
	AuthURL:      "https://accounts.google.com/o/oauth2/auth",
	TokenURL:     "https://accounts.google.com/o/oauth2/token",
	RedirectURL:  "http://max.uy/ledger/oauthcallback",
	AccessType:   "offline",
}
var RootPath = "/ledger"

var transport *oauth.Transport = &oauth.Transport{Config: oauthconfig}

func Log(message string, a ...interface{}) {
	message = fmt.Sprintf(message, a...)
	fmt.Printf("%v %v\n", time.Now().Format(time.Stamp), message)
}

func GetCookie(r *http.Request) CookieData {
	cookie, err := r.Cookie("auth")
	var data CookieData
	if err == nil {
		value, _ := url.QueryUnescape(cookie.Value)
		json.Unmarshal([]byte(value), &data)
	} else {
		Log("GetCookie error %v", err)
	}
	return data
}

func SetCookie(w http.ResponseWriter, token oauth.Token, email string) {
	cookie := CookieData{token, email}
	b, _ := json.Marshal(cookie)
	value := string(b)
	c := http.Cookie{Name: "auth", Value: url.QueryEscape(value)}
	c.Path = RootPath
	http.SetCookie(w, &c)
	validCookies = append(validCookies, cookie)
}

func handleWithTemplate(template string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ledger := mux.Vars(r)["ledger"]
		email := GetCookie(r).Email
		data := map[string]interface{}{
			"ledger":  ledger,
			"ledgers": AuthLedgers(email),
			"query":   r.FormValue("query"),
			"email":   email,
			"root":    RootPath,
		}
		if len(ledger) > 0 {
			UpdateLedger(ledger)
			data["accounts"] = LedgerAccounts(ledger)
			data["ledgerFile"] = ReadLedger(ledger)
			data["balance"] = LedgerExec(ledger, "bal assets")
		}
		if template == "query" {
			data["result"] = LedgerExec(ledger, r.FormValue("query"))
		}
		RenderTemplate(w, template, data)
	}
}

func handleLogin(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie := GetCookie(r)
		transport.Token = &cookie.Token
		if len(cookie.Email) == 0 || getEmail() != cookie.Email {
			http.Redirect(w, r, oauthconfig.AuthCodeURL(""), http.StatusFound)
		} else {
			SetCookie(w, *transport.Token, cookie.Email)

			ledger := mux.Vars(r)["ledger"]
			if len(ledger) > 0 && !AuthLedger(ledger, cookie.Email) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte("Unauthorized"))
			} else {
				handler(w, r)
			}
		}
	}
}

func editLedger(w http.ResponseWriter, r *http.Request) {
	Log("Edit ledger")
	ledger := mux.Vars(r)["ledger"]
	file := r.FormValue("file")
	if len(file) == 0 || file[len(file)-1] != '\n' {
		file += "\n"
	}
	strings.Replace(file, "\r\n", "\n", -1)

	WriteLedger(ledger, file, "webledger <"+GetCookie(r).Email+">")
	handleWithTemplate("edit")(w, r)
}

func getEmail() string {
	for _, c := range validCookies {
		if c.Token.AccessToken == transport.Token.AccessToken {
			return c.Email
		}
	}

	client := transport.Client()
	service, err := googleAuth.New(client)
	if err != nil {
		Log("Error %v", err)
		return ""
	}
	tokenInfo, err := service.Tokeninfo().Access_token(transport.AccessToken).Do()
	if err != nil {
		Log("Error %v", err)
		return ""
	}
	return tokenInfo.Email
}

func oauthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	transport.Exchange(code)
	SetCookie(w, *transport.Token, getEmail())
	http.Redirect(w, r, RootPath, http.StatusFound)
}

func logout(w http.ResponseWriter, r *http.Request) {
	c := http.Cookie{Name: "auth", Value: ""}
	http.SetCookie(w, &c)
	w.Write([]byte("Logout"))
}

func main() {
	InitLedgers()
	InitTemplates()

	ledgers_regex := ""
	for l, _ := range Ledgers() {
		if len(ledgers_regex) > 0 {
			ledgers_regex += "|"
		}
		ledgers_regex += l
	}

	router := mux.NewRouter()
	router.HandleFunc("/", handleLogin(handleWithTemplate("index"))).Methods("GET")
	router.HandleFunc("/oauthcallback", oauthCallback).Methods("GET")
	router.HandleFunc("/logout", logout).Methods("GET")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}", handleLogin(handleWithTemplate("edit"))).Methods("GET")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}", handleLogin(editLedger)).Methods("POST")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/query", handleLogin(handleWithTemplate("query"))).Methods("GET")
	router.Handle("/{path:.*}", http.FileServer(http.Dir("public")))
	http.Handle("/", router)
	http.ListenAndServe(":8082", nil)
}
