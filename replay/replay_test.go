package replay

import "testing"

func TestLogTitle(t *testing.T) {
	logTitle([]byte(`1 fda9138b7f0000016ac0ad3e 1621835869410250000 0`), "POST", "/solr/demo")
}
