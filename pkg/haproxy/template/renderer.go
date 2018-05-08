package template

import (
	"bytes"
	"strings"

	"github.com/appscode/go/log"
	hpi "github.com/appscode/voyager/pkg/haproxy/api"
	"os/exec"
	"io/ioutil"
	"github.com/pkg/errors"
)

func RenderConfig(data hpi.TemplateData) (string, error) {
	data.Canonicalize()
	if err := data.IsValid(); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err := haproxyTemplate.ExecuteTemplate(&buf, "haproxy.cfg", data)
	if err != nil {
		log.Error(err)
		return "", err
	}
	lines := strings.Split(buf.String(), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n"), nil
}

func CheckRenderedConfig(cfg string) error {
	if err := ioutil.WriteFile("/tmp/config.cfg", []byte(cfg), 0777); err != nil {
		return err
	}
	if output, err := exec.Command("haproxy", "-c", "-f", "/tmp/config.cfg").CombinedOutput(); err != nil {
		return errors.Errorf("invalid haproxy configuration, reason: %s, message: %s", err, string(output))
	}
	return nil
}
