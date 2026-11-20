package auth

import (
	"bytes"
	"fmt"
	"html/template"
	"net/netip"
	"strings"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/emails"
)

const newDeviceSubject = "Entraron a tu cuenta desde un dispositivo nuevo"

type alertData struct {
	When     time.Time
	Device   string
	IP       string
	Method   LoginMethod
	LoginURL string
}

func methodLabel(m LoginMethod) string {
	switch m {
	case MethodTOTP:
		return "código del autenticador"
	case MethodRecovery:
		return "código de recuperación"
	case MethodSetup:
		return "alta del autenticador"
	}
	return string(m)
}

var newDeviceHTML = template.Must(template.New("new-device").Parse(`<!doctype html>
<html lang="es"><body style="font-family:sans-serif;line-height:1.5">
<h1>Nuevo inicio de sesión</h1>
<p>Alguien entró a Auto Factura desde un dispositivo que no usaste antes.</p>
<p>Fecha: {{.Date}}<br>
Dispositivo: {{.Device}}<br>
{{if .IP}}Dirección IP: {{.IP}}<br>
{{end}}Método: {{.Method}}</p>
<p>Si fuiste vos, no tenés que hacer nada.<br>
Si no fuiste vos, cambiá la contraseña y cerrá todas las sesiones desde la consola:<br>
<code>admin reset-password</code><br>
<code>admin revoke-sessions</code></p>
<p><a href="{{.LoginURL}}">Abrir Auto Factura</a></p>
</body></html>
`))

type alertView struct {
	Date     string
	Device   string
	IP       string
	Method   string
	LoginURL string
}

func newDeviceMessage(to string, d alertData) (emails.Message, error) {
	v := alertView{
		Date:     d.When.Format("02/01/2006 15:04"),
		Device:   d.Device,
		IP:       validIP(d.IP),
		Method:   methodLabel(d.Method),
		LoginURL: d.LoginURL,
	}
	var html bytes.Buffer
	if err := newDeviceHTML.Execute(&html, v); err != nil {
		return emails.Message{}, fmt.Errorf("render new device email: %w", err)
	}
	var text strings.Builder
	text.WriteString("Nuevo inicio de sesión\n\n")
	text.WriteString("Alguien entró a Auto Factura desde un dispositivo que no usaste antes.\n\n")
	text.WriteString("Fecha: " + v.Date + "\n")
	text.WriteString("Dispositivo: " + v.Device + "\n")
	if v.IP != "" {
		text.WriteString("Dirección IP: " + v.IP + "\n")
	}
	text.WriteString("Método: " + v.Method + "\n\n")
	text.WriteString("Si fuiste vos, no tenés que hacer nada.\n")
	text.WriteString("Si no fuiste vos, cambiá la contraseña y cerrá todas las sesiones desde la consola:\n")
	text.WriteString("admin reset-password\nadmin revoke-sessions\n\n")
	text.WriteString("Abrir Auto Factura: " + v.LoginURL + "\n")
	return emails.Message{Kind: emails.KindDeviceAlert, To: []string{to}, Subject: newDeviceSubject, HTML: html.String(), Text: text.String()}, nil
}

func validIP(s string) string {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return ""
	}
	return addr.String()
}
