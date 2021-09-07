package r53

import (
	"fmt"
	"net"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
)

func getHostedZoneId(domainName string, svc *route53.Route53) (string, error) {
	if !strings.HasSuffix(domainName, ".") {
		domainName += "."
	}

	input := &route53.ListHostedZonesInput{}
	out, err := svc.ListHostedZones(input)
	if err != nil {
		return "", err
	}
	for _, hz := range out.HostedZones {
		if hz == nil || hz.Name == nil {
			continue
		}
		if *hz.Name == domainName {
			return strings.ReplaceAll(*hz.Id, "/hostedzone/", ""), nil
		}

	}

	return "", fmt.Errorf("domain %s not found", domainName)
}

type Record struct {
	FQDN         string
	hostedZoneId string
	svc          *route53.Route53
}

func (r Record) Upsert(ip net.IP) error {
	input := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String("UPSERT"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String(r.FQDN),
						ResourceRecords: []*route53.ResourceRecord{
							{
								Value: aws.String(ip.String()),
							},
						},
						TTL:  aws.Int64(60),
						Type: aws.String("A"),
					},
				},
			},
			Comment: aws.String(""),
		},
		HostedZoneId: aws.String(r.hostedZoneId),
	}
	_, err := r.svc.ChangeResourceRecordSets(input)
	if err != nil {
		return fmt.Errorf("cannot upsert %s: %w", r.FQDN, err)
	}

	return nil

}

func NewRecord(hostName, domainName string, svc *route53.Route53) (Record, error) {
	hzi, err := getHostedZoneId(domainName, svc)
	if err != nil {
		return Record{}, fmt.Errorf("cannot create 'Record': %w", err)
	}

	fqdn := strings.Join([]string{hostName, domainName}, ".")
	if !strings.HasSuffix(fqdn, ".") {
		fqdn += "."
	}

	return Record{
		FQDN:         fqdn,
		svc:          svc,
		hostedZoneId: hzi,
	}, nil
}
