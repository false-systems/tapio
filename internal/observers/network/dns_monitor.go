package network

import (
	"fmt"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// DNSQuery represents a parsed DNS query
type DNSQuery struct {
	QueryID    uint16
	DomainName string
	QueryType  string
	Timestamp  time.Time
}

// DNSResponse represents a parsed DNS response
type DNSResponse struct {
	QueryID      uint16
	ResponseCode string
	Answers      []string
	Latency      time.Duration
}

// parseDNSQuery parses a DNS query packet
func parseDNSQuery(packet []byte) (*DNSQuery, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS message: %w", err)
	}

	question, err := parser.Question()
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS question: %w", err)
	}

	return &DNSQuery{
		QueryID:    header.ID,
		DomainName: question.Name.String(),
		QueryType:  normalizeQueryType(question.Type),
	}, nil
}

// parseDNSResponse parses a DNS response packet
func parseDNSResponse(packet []byte) (*DNSResponse, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS message: %w", err)
	}

	response := &DNSResponse{
		QueryID:      header.ID,
		ResponseCode: rcodeToString(header.RCode),
		Answers:      []string{},
	}

	// Skip questions
	if err := parser.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("failed to skip questions: %w", err)
	}

	// Parse answers
	for {
		answer, err := parser.Answer()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse answer: %w", err)
		}

		// Extract IP address from A record
		if aRecord, ok := answer.Body.(*dnsmessage.AResource); ok {
			ip := fmt.Sprintf("%d.%d.%d.%d",
				aRecord.A[0], aRecord.A[1], aRecord.A[2], aRecord.A[3])
			response.Answers = append(response.Answers, ip)
		}

		// Extract IPv6 address from AAAA record
		if aaaaRecord, ok := answer.Body.(*dnsmessage.AAAAResource); ok {
			ip := fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
				uint16(aaaaRecord.AAAA[0])<<8|uint16(aaaaRecord.AAAA[1]),
				uint16(aaaaRecord.AAAA[2])<<8|uint16(aaaaRecord.AAAA[3]),
				uint16(aaaaRecord.AAAA[4])<<8|uint16(aaaaRecord.AAAA[5]),
				uint16(aaaaRecord.AAAA[6])<<8|uint16(aaaaRecord.AAAA[7]),
				uint16(aaaaRecord.AAAA[8])<<8|uint16(aaaaRecord.AAAA[9]),
				uint16(aaaaRecord.AAAA[10])<<8|uint16(aaaaRecord.AAAA[11]),
				uint16(aaaaRecord.AAAA[12])<<8|uint16(aaaaRecord.AAAA[13]),
				uint16(aaaaRecord.AAAA[14])<<8|uint16(aaaaRecord.AAAA[15]))
			response.Answers = append(response.Answers, ip)
		}
	}

	return response, nil
}

// detectDNSProblem detects DNS problems based on query and response
func detectDNSProblem(query *DNSQuery, response *DNSResponse) string {
	// NXDOMAIN - domain doesn't exist
	if response.ResponseCode == "NXDOMAIN" {
		return "dns_nxdomain"
	}

	// SERVFAIL - DNS server error
	if response.ResponseCode == "SERVFAIL" {
		return "dns_servfail"
	}

	// Slow query (>100ms)
	if response.Latency > 100*time.Millisecond {
		return "dns_slow_query"
	}

	// Success
	return "dns_success"
}

// normalizeQueryType converts DNS type to short string (TypeA -> A)
func normalizeQueryType(qtype dnsmessage.Type) string {
	switch qtype {
	case dnsmessage.TypeA:
		return "A"
	case dnsmessage.TypeAAAA:
		return "AAAA"
	case dnsmessage.TypeCNAME:
		return "CNAME"
	case dnsmessage.TypeMX:
		return "MX"
	case dnsmessage.TypeNS:
		return "NS"
	case dnsmessage.TypePTR:
		return "PTR"
	case dnsmessage.TypeSOA:
		return "SOA"
	case dnsmessage.TypeSRV:
		return "SRV"
	case dnsmessage.TypeTXT:
		return "TXT"
	default:
		return qtype.String()
	}
}

// rcodeToString converts DNS response code to string
func rcodeToString(rcode dnsmessage.RCode) string {
	switch rcode {
	case dnsmessage.RCodeSuccess:
		return "NOERROR"
	case dnsmessage.RCodeFormatError:
		return "FORMERR"
	case dnsmessage.RCodeServerFailure:
		return "SERVFAIL"
	case dnsmessage.RCodeNameError:
		return "NXDOMAIN"
	case dnsmessage.RCodeNotImplemented:
		return "NOTIMP"
	case dnsmessage.RCodeRefused:
		return "REFUSED"
	default:
		return fmt.Sprintf("RCODE_%d", rcode)
	}
}
