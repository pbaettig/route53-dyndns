package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/pbaettig/route53-dyndns/r53"
)

var (
	MoreThanOneIpErr = fmt.Errorf("more than 1 IP found in DNS")
	shutdown         = make(chan struct{})
	signals          = make(chan os.Signal, 1)

	flagHostName   string
	flagDomainName string
)

const (
	targetRecord = "home.caspal.ch"
)

type IpChange struct {
	Timestamp time.Time
	OldIP     net.IP
	NewIP     net.IP
}

func signalHandler() {
	for {
		sig := <-signals
		log.Printf("%s received", sig.String())

		if sig == os.Interrupt {
			// one for ipChecker
			shutdown <- struct{}{}
			return
		}
	}
}

func getIP() (net.IP, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return net.IP{}, err
	}

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return net.IP{}, err
	}

	return net.ParseIP(string(buf)), nil

}

func ipChecker(initial net.IP, out chan<- IpChange, done <-chan struct{}) {
	defer close(out)
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case t := <-ticker.C:
			currentIP, err := getIP()
			if err != nil {
				log.Printf("couldn't determine IP: %s. This could be a transient issue and is ignored.\n", err.Error())
				continue
			}

			if currentIP.Equal(initial) {
				continue
			}

			out <- IpChange{
				Timestamp: t,
				OldIP:     initial,
				NewIP:     currentIP,
			}
			initial = currentIP
		}
	}
}

func lookupHost(name string) (net.IP, error) {
	ips, err := net.LookupIP(name)
	if err != nil {
		return net.IP{}, err
	}

	if len(ips) > 1 {
		return ips[0], MoreThanOneIpErr
	}

	return ips[0], nil
}

func parseFlags() error {
	flag.StringVar(&flagHostName, "host", "", "The name of the record that should be updated on public IP change")
	flag.StringVar(&flagDomainName, "domain", "", "The domain name containing the record that should be updated on public IP change")
	flag.Parse()

	if flagHostName == "" {
		return fmt.Errorf("-host is a required parameter")
	}
	if flagDomainName == "" {
		return fmt.Errorf("-domain is a required parameter")
	}

	return nil
}

func main() {
	err := parseFlags()
	if err != nil {
		log.Fatalln(err.Error())
	}

	signal.Notify(signals, os.Interrupt)
	go signalHandler()

	ipChanges := make(chan IpChange)

	sess, err := session.NewSession(&aws.Config{})
	if err != nil {
		log.Fatalln(err.Error())
	}

	record, err := r53.NewRecord(flagHostName, flagDomainName, route53.New(sess))
	if err != nil {
		log.Fatal(err.Error())
	}

	publicIp, err := getIP()
	if err == nil {
		dnsIp, err := lookupHost(record.FQDN)
		if err == nil {
			if !publicIp.Equal(dnsIp) {
				log.Printf("%s resolves to %s but the current systems IP is %s. Updating the record.\n",
					record.FQDN, dnsIp.String(), publicIp.String())
				record.Upsert(publicIp)
			}
		} else {
			log.Printf("%s does not exist and will be created pointing to the current public IP (%s) of the system.\n",
				record.FQDN, publicIp.String())
			record.Upsert(publicIp)
		}
	}

	go ipChecker(publicIp, ipChanges, shutdown)

	log.Println("waiting for changes to the systems public IP...")
	for c := range ipChanges {
		log.Printf("IP change detected: %s -> %s\n", c.OldIP, c.NewIP)
		err := record.Upsert(c.NewIP)
		if err != nil {
			log.Printf("ERROR: %s\n", err.Error())
		}
	}

	log.Println("Bye!")
}
