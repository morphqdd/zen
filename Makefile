run:
	doas go run ./cmd/client.go -remote 144.31.185.50 -port 5354

up:
	doas ip link set zen-tun up
	doas ip route add 0.0.0.0/1 dev zen-tun

