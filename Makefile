all : gyagowin.exe

gyagowin.exe : main.go gyagowin.syso
	go build -ldflags="-H windowsgui"

gyagowin.syso : gyagowin.rc
	windres gyagowin.rc gyagowin.syso
