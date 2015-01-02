GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -o build/main.linux main.go tokens.go ledger.go gzip.go templates.go
ssh server <<'ENDSSH'
  mkdir -p /var/www/webledger/
ENDSSH
scp build/main.linux server:/var/www/webledger/main.linux.next
scp deploy/webledger.conf server:/etc/init/
ssh server <<'ENDSSH'
  /sbin/stop webledger
  mv /var/www/webledger/main.linux.next /var/www/webledger/main.linux
  /sbin/start webledger
ENDSSH

