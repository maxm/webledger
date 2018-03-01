GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -o build/main.linux main.go tokens.go ledger.go gzip.go templates.go
ssh server <<'ENDSSH'
  mkdir -p /var/www/webledger/
ENDSSH
scp build/main.linux server:/var/www/webledger/main.linux.next
scp -r public/ server:/var/www/webledger/
scp -r templates/ server:/var/www/webledger/
scp ledgers.json server:/var/www/webledger/
scp deploy/webledger.service server:/etc/systemd/system/
ssh server <<'ENDSSH'
  systemctl stop webledger
  mv /var/www/webledger/main.linux.next /var/www/webledger/main.linux
  systemctl start webledger
  systemctl enable webledger
ENDSSH

