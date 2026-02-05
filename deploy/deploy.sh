GOOS=linux GOARCH=386 CGO_ENABLED=0 go build -o build/main.linux
ssh server <<'ENDSSH'
  mkdir -p /var/www/webledger/
ENDSSH
rsync -avP build/main.linux server:/var/www/webledger/main.linux.next
rsync -avP -r public server:/var/www/webledger/
rsync -avP templates server:/var/www/webledger/
rsync -avP ledgers.json server:/var/www/webledger/
rsync -avP deploy/webledger.service server:/etc/systemd/system/
ssh server <<'ENDSSH'
  systemctl stop webledger
  mv /var/www/webledger/main.linux.next /var/www/webledger/main.linux
  systemctl start webledger
  systemctl enable webledger
ENDSSH

