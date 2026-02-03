package main

import (
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"net/http"
	"net/url"
	"strings"
	"time"
	"context"
	"io/ioutil"
	"bytes"
	"os"
)

type CookieData struct {
	Token oauth2.Token
	Email string
}

var validCookies []CookieData = []CookieData{}

// isLocalEnvironment checks if we're running in a local development environment
func isLocalEnvironment() bool {
	// Check for environment variable first
	if env := os.Getenv("WEBLEDGER_ENV"); env == "production" {
		return false
	} else if env == "local" || env == "development" {
		return true
	}
	
	// Check hostname - if it contains "localhost" or is a local IP, assume local
	hostname, err := os.Hostname()
	if err == nil {
		hostnameUpper := strings.ToLower(hostname)
		if strings.Contains(hostnameUpper, "localhost") || 
		   strings.Contains(hostnameUpper, "macbook") ||
		   strings.Contains(hostnameUpper, "local") {
			return true
		}
	}
	
	// Default to production for safety
	return false
}

var oauthconfig *oauth2.Config
var RootPath string

const oauthGoogleUrlAPI = "https://www.googleapis.com/oauth2/v2/userinfo?access_token="

func initConfig() {
	if isLocalEnvironment() {
		// Local development settings
		RootPath = ""
		oauthconfig = &oauth2.Config{
			ClientID:     ClientId,
			ClientSecret: ClientSecret,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
			Endpoint:     google.Endpoint,
			RedirectURL:  "http://localhost:8082/oauthcallback",
		}
		Log("Running in LOCAL mode")
	} else {
		// Production settings
		RootPath = "/ledger"
		oauthconfig = &oauth2.Config{
			ClientID:     ClientId,
			ClientSecret: ClientSecret,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
			Endpoint:     google.Endpoint,
			RedirectURL:  "https://max.uy/ledger/oauthcallback",
		}
		Log("Running in PRODUCTION mode")
	}
}

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

func SetCookie(w http.ResponseWriter, token oauth2.Token, email string) {
	cookie := CookieData{token, email}
	b, _ := json.Marshal(cookie)
	value := string(b)
	c := http.Cookie{Name: "auth", Value: url.QueryEscape(value)}
	c.Path = RootPath
	http.SetCookie(w, &c)
	validCookies = append(validCookies, cookie)
}

func handleWithTemplateAndData(template string, fillData func(map[string]interface{})) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ledger := mux.Vars(r)["ledger"]
		email := GetCookie(r).Email
		data := map[string]interface{}{
			"ledger":  ledger,
			"ledgers": AuthLedgers(email),
			"query":   r.FormValue("query"),
			"email":   email,
			"root":    RootPath,
			"cookies": r.Header.Get("Cookie"),
		}
		if len(ledger) > 0 {
			UpdateLedger(ledger)
			data["accounts"] = LedgerAccounts(ledger)
			data["ledgerFile"] = ReadLedger(ledger)
			data["balance"] = LedgerExec(ledger, "bal assets")
		}
		fillData(data)
		if template == "query" {
			data["result"] = LedgerExec(ledger, r.FormValue("query"))
		}
		RenderTemplate(w, template, data)
	}
}

func handleWithTemplate(template string) func(http.ResponseWriter, *http.Request) {
	return handleWithTemplateAndData(template, func(map[string]interface{}) { })
}

func monthlyData(data map[string]interface{}) {
	ledger := data["ledger"].(string)
	
	now := time.Now()
	this_month := now.Format("Jan")
	this_year := now.Format("2006")
	last_year := now.AddDate(-1, 0, 0).Format("2006")

	data["yearly_expense"] = LedgerExec(ledger, "bal expenses and not retiro and not bono -e '" + this_month + " " + this_year + "' -b '" + this_month + " " + last_year + "' -X US$ -H --depth 1  -F '%T'")
	data["monthly_expense"] =  LedgerExec(ledger, "bal expenses and not retiro and not bono -e '" + this_month + " " + this_year + "' -b '" + this_month + " " + last_year + "' -X US$ -H --depth 1  -F '%(T/12)'")

	last_month := now.AddDate(0, -1, 0).Format("Jan 2006")
	data["last_month"] = last_month
	data["last_month_income"] = LedgerExec(ledger, "bal income -p '" + last_month + "' -X US$ -H --depth 1  -F '%(-T)'")

	last_last_month := now.AddDate(0, -2, 0).Format("Jan 2006")
	data["last_last_month"] = last_last_month
	data["last_last_month_income"] = LedgerExec(ledger, "bal income -p '" + last_last_month + "' -X US$ -H --depth 1  -F '%(-T)'")

	data["bank_balance"] = LedgerExec(ledger, "bal assets:bancos -X US$ -H --depth 1  -F '%T'")
}

func handleRaw(w http.ResponseWriter, r *http.Request) {
	ledger := mux.Vars(r)["ledger"]
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(ReadLedger(ledger)))
}

func handleQueryText(w http.ResponseWriter, r *http.Request) {
	ledger := mux.Vars(r)["ledger"]
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(LedgerExec(ledger, r.FormValue("query"))))
}

func handleLogin(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie := GetCookie(r)
		if len(cookie.Email) == 0 {
			http.Redirect(w, r, oauthconfig.AuthCodeURL("randomtoken", oauth2.AccessTypeOffline, oauth2.ApprovalForce), http.StatusFound)
			return
		}
		
		// Refresh token if expired or about to expire
		if time.Until(cookie.Token.Expiry) < 5*time.Minute {
			Log("Token expired or expiring soon, refreshing...")
			tokenSource := oauthconfig.TokenSource(context.Background(), &cookie.Token)
			newToken, err := tokenSource.Token()
			if err != nil {
				Log("Token refresh failed: %v", err)
				http.Redirect(w, r, oauthconfig.AuthCodeURL("randomtoken", oauth2.AccessTypeOffline, oauth2.ApprovalForce), http.StatusFound)
				return
			}
			SetCookie(w, *newToken, cookie.Email)
			cookie.Token = *newToken
			Log("Token refreshed successfully")
		}
		
		// Verify email still matches
		if getEmail(cookie.Token) != cookie.Email {
			Log("Email mismatch, redirecting to login")
			http.Redirect(w, r, oauthconfig.AuthCodeURL("randomtoken", oauth2.AccessTypeOffline, oauth2.ApprovalForce), http.StatusFound)
			return
		}
		
		ledger := mux.Vars(r)["ledger"]
		if len(ledger) > 0 && !AuthLedger(ledger, cookie.Email) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized"))
		} else {
			handler(w, r)
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

func handleAppend(w http.ResponseWriter, r *http.Request) {
	Log("Append")
	ledger := mux.Vars(r)["ledger"]
	file := ReadLedger(ledger)

	strings.Replace(file, "\r\n", "\n", -1)
	for len(file) < 2 || file[len(file)-1] != '\n' || file[len(file)-2] != '\n' {
		file += "\n"
	}
	file += strings.TrimSpace(r.FormValue("append"))

	if len(file) == 0 || file[len(file)-1] != '\n' {
		file += "\n"
	}

	WriteLedger(ledger, file, "webledger <"+GetCookie(r).Email+">")
	handleRaw(w, r)
}

func getEmail(token oauth2.Token) string {
	for _, c := range validCookies {
		if c.Token.AccessToken == token.AccessToken {
			return c.Email
		}
	}

	response, err := http.Get(oauthGoogleUrlAPI + token.AccessToken)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		Log("Error getting email: %v", err)
		return ""
	}
	if (contents == nil) {
		Log("Error getting email: contents is nil")
		return ""
	}
	var result map[string]interface{}
	json.Unmarshal([]byte(contents), &result)
	if (result == nil || result["email"] == nil) {
		Log("Error getting email: result is nil")
		return ""
	}
	return result["email"].(string);
}

func oauthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	tok, _ := oauthconfig.Exchange(context.Background(), code)
	SetCookie(w, *tok, getEmail(*tok))
	http.Redirect(w, r, RootPath, http.StatusFound)
}

func logout(w http.ResponseWriter, r *http.Request) {
	c := http.Cookie{Name: "auth", Value: ""}
	http.SetCookie(w, &c)
	w.Write([]byte("Logout"))
}

func handleReconcile(w http.ResponseWriter, r *http.Request) {
	ledger := mux.Vars(r)["ledger"]
	email := GetCookie(r).Email
	
	// Get bank accounts from ledger (accounts starting with Assets:Bank)
	allAccounts := LedgerAccounts(ledger)
	bankAccounts := []string{}
	for _, acc := range allAccounts {
		if strings.HasPrefix(acc, "Assets:Bank") {
			bankAccounts = append(bankAccounts, acc)
		}
	}
	
	data := map[string]interface{}{
		"ledger":       ledger,
		"ledgers":      AuthLedgers(email),
		"email":        email,
		"root":         RootPath,
		"bankAccounts": bankAccounts,
	}
	RenderTemplate(w, "reconcile", data)
}

func handleReconcileUpload(w http.ResponseWriter, r *http.Request) {
	ledger := mux.Vars(r)["ledger"]
	
	// Parse multipart form
	err := r.ParseMultipartForm(10 << 20) // 10 MB max
	if err != nil {
		http.Error(w, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}
	
	file, header, err := r.FormFile("statement")
	if err != nil {
		http.Error(w, "Error retrieving file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	
	// Read file into memory
	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Detect bank from filename or form parameter
	bankAccount := r.FormValue("account")
	if bankAccount == "" {
		bankAccount = DetectBankFromFilename(header.Filename)
	}
	if bankAccount == "" {
		http.Error(w, "Could not detect bank account. Please select manually.", http.StatusBadRequest)
		return
	}
	
	// Parse bank statement
	var statement *BankStatement
	fileExtension := strings.ToLower(header.Filename[strings.LastIndex(header.Filename, ".")+1:])
	
	if fileExtension == "xls" || fileExtension == "xlsx" {
		reader := bytes.NewReader(fileBytes)
		
		if strings.Contains(bankAccount, "BROU") {
			statement, err = ParseBrouStatement(reader)
		} else if strings.Contains(bankAccount, "Itau") {
			statement, err = ParseItauStatement(reader)
		} else {
			http.Error(w, "Unknown bank account type", http.StatusBadRequest)
			return
		}
		
		if err != nil {
			http.Error(w, "Error parsing statement: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else if fileExtension == "csv" {
		reader := bytes.NewReader(fileBytes)
		statement, err = ParseBankStatementCSV(reader, bankAccount)
		if err != nil {
			http.Error(w, "Error parsing CSV: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "Unsupported file format. Please upload .xls or .csv", http.StatusBadRequest)
		return
	}
	
	// Query ledger transactions for this account with currency filter
	ledgerTransactions, err := QueryLedgerTransactions(ledger, bankAccount, statement.Currency)
	if err != nil {
		http.Error(w, "Error querying ledger: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Perform reconciliation
	result := ReconcileBankStatement(statement, ledgerTransactions)
	
	// Prepare data for template
	email := GetCookie(r).Email
	data := map[string]interface{}{
		"ledger":          ledger,
		"ledgers":         AuthLedgers(email),
		"email":           email,
		"root":            RootPath,
		"result":          result,
		"bankAccount":     bankAccount,
		"suggestedEntries": GenerateLedgerEntries(result.UnmatchedBank),
	}
	
	RenderTemplate(w, "reconcile_result", data)
}

func main() {
	initConfig()
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
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/query_text", handleLogin(handleQueryText)).Methods("GET")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/app_auth", handleLogin(handleWithTemplate("app_auth"))).Methods("GET")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/raw", handleLogin(handleRaw)).Methods("GET")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/append", handleLogin(handleAppend)).Methods("POST")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/monthly", handleLogin(handleWithTemplateAndData("monthly", monthlyData))).Methods("GET")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/reconcile", handleLogin(handleReconcile)).Methods("GET")
	router.HandleFunc("/{ledger:"+ledgers_regex+"}/reconcile", handleLogin(handleReconcileUpload)).Methods("POST")
	router.Handle("/{path:.*}", http.FileServer(http.Dir("public")))
	http.Handle("/", router)
	http.ListenAndServe(":8082", nil)
}
