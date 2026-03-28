package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"zen/internal/utils"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/songgao/water"
	"golang.org/x/net/ipv4"
)

var (
	remoteIP = flag.String("remote", "", "Remote server (external) IP like 8.8.8.8")
	port     = flag.Int("port", 4321, "TCP port for communication")
)

const (
	BUFFER_SIZE = 1500
	MTU         = "1300"
	LOCAL_IP    = "0.0.0.0/1"
)

func main() {
	flag.Parse()
	config := water.Config{
		DeviceType: water.TUN,
	}

	if "" == *remoteIP {
		flag.Usage()
		log.Fatalln("\nremote server is not specified")
	}

	config.Name = "zen-tun"

	iface, err := water.New(config)

	log.Println("Interface allocated:", iface.Name())
	// set interface parameters
	utils.RunIP("link", "set", "dev", iface.Name(), "mtu", MTU)
	utils.RunIP("link", "set", "dev", iface.Name(), "up")
	utils.RunIP("route", "add", LOCAL_IP, "dev", iface.Name())
	if err != nil {
		log.Fatalln(err)
	}

	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%v", *remoteIP, *port))
	if err != nil {
		log.Fatalln("Unable to get socket:", err)
	}
	lstnAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%v", *port))
	if err != nil {
		log.Fatalln("Unable to get socket:", err)
	}

	lstnConn, err := net.ListenUDP("udp", lstnAddr)
	if err != nil {
		log.Fatalln("Unable to listen socket:", err)
	}
	defer lstnConn.Close()

	go func() {
		buf := make([]byte, BUFFER_SIZE)
		for {
			n, addr, err := lstnConn.ReadFromUDP(buf)
			header, _ := ipv4.ParseHeader(buf[:n])
			fmt.Printf("Received %d bytes from %v: %+v\n", n, addr, header)

			if err != nil || n == 0 {
				fmt.Println(err)
				continue
			}

			iface.Write(buf[:n])
		}
	}()

	packet := make([]byte, BUFFER_SIZE)

	for {
		n, err := iface.Read(packet)
		if err != nil {
			log.Fatalln(err)
		}

		lstnConn.WriteToUDP(packet[:n], remoteAddr)
		packet := gopacket.NewPacket(packet, layers.LayerTypeIPv4, gopacket.Default)

		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer != nil {
			ip, _ := ipLayer.(*layers.IPv4)
			log.Println("Src: ", ip.SrcIP)
			log.Println("Dst: ", ip.DstIP)
			log.Println("LayerType: ", ip.LayerType())

			packet := gopacket.NewPacket(ip.LayerContents(), layers.LayerTypeTCP, gopacket.Default)

			udpLayer := packet.Layer(layers.LayerTypeUDP)
			if udpLayer != nil {
				udp, _ := udpLayer.(*layers.UDP)
				log.Println("Src port: ", udp.SrcPort)
				log.Println("Dst port: ", udp.DstPort)
				log.Println("LayerType: ", udp.LayerType())
				log.Printf("Content: %s\n", udp.LayerContents())
			}
		}
	}
}
