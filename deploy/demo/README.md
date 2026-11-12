# Disposable demo

```sh
./deploy/demo/reset.sh
```

The demo runs the published image on port 8080 with a throwaway volume. Run the
reset script from cron/systemd every hour to wipe all data and print a fresh
setup token. Never put real credentials in a public demo.
