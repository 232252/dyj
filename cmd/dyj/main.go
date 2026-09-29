// Command dyj shares a printer over the LAN as a driverless IPP
// Everywhere service, and prints to such services as a client.
//
// One binary, two roles:
//
//	dyj serve                 run the print server (mDNS + IPP + backend)
//	dyj browse                find _ipp._tcp printers on the LAN
//	dyj status <uri>          query a printer's attributes
//	dyj print <uri> <file>    submit a document (txt/jpg/png/pwg)
//	dyj jobs <uri>            list the printer's job queue
//	dyj cancel <uri> <id>     cancel a job
//	dyj service ...           install/run as a system service
//
// This binary is an assembly point only: it parses flags and wires
// packages together. All protocol logic lives in pkg/.
package main

import (
	"fmt"
	"os"
)

// dyjVersion is stamped into the admin API and diagnostics. Keep in
// sync with the latest CHANGELOG entry.
const dyjVersion = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "browse":
		err = cmdBrowse(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "print":
		err = cmdPrint(os.Args[2:])
	case "jobs":
		err = cmdJobs(os.Args[2:])
	case "cancel":
		err = cmdCancel(os.Args[2:])
	case "service":
		err = cmdService(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "tray":
		err = cmdTray(os.Args[2:])
	case "list-interfaces":
		err = cmdListInterfaces()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "dyj: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dyj: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`dyj — LAN printer sharing, driverless (IPP Everywhere + mDNS)

Usage:
  dyj serve    [flags]                    run the print server
  dyj browse   [--timeout 5s]             find printers via mDNS
  dyj status   <uri>                      query printer attributes
  dyj print    [flags] <uri> <file>       submit a document
  dyj jobs     <uri>                      list jobs
  dyj cancel   <uri> <job-id>             cancel a job
  dyj service  install|uninstall|start|stop|run   manage the system service
  dyj doctor   [flags]                    diagnose this machine (firewall/port/mDNS/backend)
  dyj tray     [-port N]                  run the system tray (Windows)
  dyj list-interfaces                     show multicast-capable interfaces

Admin UI: start the server and open http://127.0.0.1:<port>/admin —
configure, watch jobs and logs from any device on the LAN.

Server flags (serve):
  -name NAME        printer name shown to clients (default "dyj on <host>")
  -port N           IPP port (default 6631; 631 needs admin rights)
  -printer NAME     Windows print queue (wingdi) or CUPS queue (cups)
  -backend NAME     wingdi | cups | file | null
                    (default: wingdi on Windows; cups when CUPS queues
                    exist, else file elsewhere; file requires -out DIR)
  -out DIR          output dir for the file backend (default ./dyj-out)
  -spool DIR        spool directory (default per-OS user data dir)
  -tls-port N       also serve ipps:// on this port (self-signed cert)
  -iface LIST       comma-separated interface names/addresses to advertise
  -no-mdns          do not advertise via mDNS
  -v                verbose logging

Client flags (print):
  -user NAME        requesting-user-name (default $USERNAME/$USER)
  -copies N         copies
  -media NAME       PWG media, e.g. iso_a4_210x297mm
  -color            ask for color
  -jobname NAME     job name shown in the queue

Client file types: .txt .jpg .png .pdf .pwg
(.pdf is sent as application/pdf; the server must advertise it —
check with 'dyj status <uri>'.)
`)
}
