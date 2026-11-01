package mailhog

import (
	"regexp"
	"strings"
	"testing"
)

func TestTextDecodesQuotedPrintableAndFindsLink(t *testing.T) {
	m := Message{
		Content: Content{Headers: map[string][]string{
			"Subject": {"=?utf-8?q?Verifizieren_Sie_Ihre_E-Mail-Adresse?="},
			"From":    {`"STACKIT Accounts" <info@accounts.stackit.cloud>`},
		}},
		To: []Path{{Mailbox: "asvic1", Domain: "uniphi.de"}},
		MIME: &MIMEBody{Parts: []Content{{
			Headers: map[string][]string{"Content-Transfer-Encoding": {"quoted-printable"}},
			Body:    "<a href=3D\"https://accounts.stack=\r\nit.cloud/ui/login/mail/verification?_r=3DPoMtKytx=\r\nfGArbgm\" style=3D\"x\">Schaltfl=C3=A4che",
		}}},
	}
	if got := m.Subject(); got != "Verifizieren Sie Ihre E-Mail-Adresse" {
		t.Errorf("subject %q", got)
	}
	if got := m.FromAddress(); got != "info@accounts.stackit.cloud" {
		t.Errorf("from %q", got)
	}
	if !m.IsTo("ASVIC1@uniphi.de") {
		t.Error("IsTo failed")
	}
	text := m.Text()
	if !strings.Contains(text, "Schaltfläche") {
		t.Errorf("umlaut not decoded: %q", text)
	}
	re := regexp.MustCompile(`https://accounts\.stackit\.cloud/ui/login/mail/verification\?_r=[A-Za-z0-9_\-]+`)
	if got := re.FindString(text); got != "https://accounts.stackit.cloud/ui/login/mail/verification?_r=PoMtKytxfGArbgm" {
		t.Errorf("link %q", got)
	}
	if !SubjectFrom("info@accounts.stackit.cloud", "Verifizieren")(&m) {
		t.Error("matcher failed")
	}
}
