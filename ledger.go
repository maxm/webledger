package main

import (
	"encoding/json"
	"github.com/mattn/go-shellwords"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
)

type LedgerDef struct {
	Url   string
	Path  string
	Users []string
}

var ledgers map[string]LedgerDef

func Ledgers() map[string]LedgerDef {
	return ledgers
}

func InitLedgers() {
	file, err := os.Open("ledgers.json")
	if err != nil {
		Log("%v", err)
		return
	}
	decoder := json.NewDecoder(file)
	decoder.Decode(&ledgers)

	root, _ := os.Getwd()

	for name, def := range ledgers {
		dir := path.Join(root, "repos", name)
		_, err = os.Stat(dir)
		if os.IsNotExist(err) {
			os.MkdirAll(dir, os.ModeDir|0700)
			Run("git", "clone", def.Url, dir)
		}
	}
}

func LedgerPath(ledger string) string {
	root, _ := os.Getwd()
	return path.Join(root, "repos", ledger, ledgers[ledger].Path)
}

func UpdateLedger(ledger string) {
	ledger_path := path.Dir(LedgerPath(ledger))
	Run(ledger_path, "git", "pull", "origin", "master")
}

func ReadLedger(ledger string) string {
	bytes, err := ioutil.ReadFile(LedgerPath(ledger))
	if err != nil {
		Log("Error reading ledger %v: %v", ledger, err)
	}
	return string(bytes)
}

func WriteLedger(ledger string, file string, author string) {
	ledger_path := LedgerPath(ledger)
	ledger_dir := path.Dir(ledger_path)
	Run(ledger_dir, "git", "pull", "origin", "master")
	err := ioutil.WriteFile(ledger_path, []byte(file), os.ModePerm)
	if err != nil {
		Log("Error %v", err)
		return
	}
	Run(ledger_dir, "git", "add", ledgers[ledger].Path)
	Run(ledger_dir, "git", "commit", "-m", "webledger", "--author", author)
	Run(ledger_dir, "git", "push", "origin", "master")
}

func Run(dir string, name string, arg ...string) {
	Log("%v %v", name, arg)
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		Log("Error %v", err)
	}
	Log(string(out))
}

func LedgerExec(ledger string, query string) string {
	parsed_query, err := shellwords.Parse(query)
	if err != nil {
		Log("Error %v", err)
		return err.Error()
	}
	params := append([]string{"-f", LedgerPath(ledger)}, parsed_query...)
	Log("ledger %v", params)
	result, err := exec.Command("ledger", params...).CombinedOutput()
	if err != nil {
		Log("Error %v", err)
	}
	return string(result)
}

func LedgerAccounts(ledger string) []string {
	re := regexp.MustCompile("(?m)^[ \\t]+(\\w.*?)([ \\t]{2}.*?)?$")
	matches := re.FindAllStringSubmatch(ReadLedger(ledger), -1)
	accounts := []string{}
	set := map[string]bool{}
	for _, match := range matches {
		account := strings.TrimSpace(match[1])
		if !set[account] {
			accounts = append(accounts, account)
			set[account] = true
		}
	}
	return accounts
}

func AuthLedger(ledger string, email string) bool {
	for _, user := range ledgers[ledger].Users {
		if user == email {
			return true
		}
	}
	return false
}

func AuthLedgers(email string) []string {
	list := []string{}
	for ledger := range ledgers {
		if AuthLedger(ledger, email) {
			list = append(list, ledger)
		}
	}
	return list
}
