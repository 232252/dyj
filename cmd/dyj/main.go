// Command dyj shares printers over the LAN as a driverless IPP Everywhere
// service, and adds such services as ordinary system printers — one
// binary, wizard-first (ADR-0010):
//
//	dyj                      first run: role wizard in the browser;
//	                        afterwards: run the configured role
//	dyj open                 open the admin UI (desktop shortcut target)
//	dyj serve   [flags]      expert: run the share side with flags
//	dyj browse              find _ipp._tcp printers on the LAN
//	dyj status <uri>         query a printer's attributes
//	dyj print <uri> <file>   submit a document (txt/jpg/png/pdf/pwg)
//	dyj jobs <uri>           list the printer's job queue
//	dyj cancel <uri> <id>    cancel a job
//	dyj service ...          install/run as a system service
//	dyj doctor               diagnose this machine
//
// This binary is an assembly point only: it parses flags and wires
// packages together. All protocol logic lives in pkg/.
package main

import (
	"fmt"
	"os"
)

// dyjVersion is stamped into the admin API and diagnostics. Keep
// sync with the latest CHANGELOG entry.
const dyjVersion = "0.4.1"

func main() {
	if len(os.Args) < 2 {
		// The foolproof path: no arguments = guided launcher.
		if err := cmdLaunch(); err != nil {
			fmt.Fprintf(os.Stderr, "dyj: %v\n", err)
			os.Exit(1)
		}
		return
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "open":
		err = cmdOpen(os.Args[2:])
	case "setup-helper": // internal, elevated on Windows
		err = cmdSetupHelper(os.Args[2:])
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
  dyj                                    guided launcher (wizard on first run)
  dyj open                               open the admin UI in the browser
  dyj serve    [flags]                   run the share side (expert path)
  dyj browse   [--timeout 5s]            find printers via mDNS
  dyj status   <uri>                     query printer attributes
  dyj print    [flags] <uri> <file>      submit a document
  dyj jobs     <uri>                     list jobs
  dyj cancel   <uri> <job-id>            cancel a job
  dyj service  install|uninstall|start|stop|run   manage the system service
  dyj doctor   [flags]                   diagnose this machine
  dyj tray     [-port N]                 run the system tray (Windows)
  dyj list-interfaces                    show multicast-capable interfaces

First run opens a wizard with two choices:
  share local printers  — auto-enumerate this machine's queues and share
                          them; a 4-digit pairing code guards the LAN side
  add network printer   — scan the LAN, pair with the code, and the printer
                          is installed as a normal system queue (WPS, Word,
                          notepad print to it directly; dyj stays out of
                          the path)

Admin UI: http://127.0.0.1:<port>/admin — configure, watch jobs and logs,
manage paired devices from any device on the LAN.

Server flags (serve):
  -name NAME        printer name shown to clients (default "dyj on <host>")
  -port N           IPP port (default 6631; 631 needs admin rights)
  -printer NAME     Windows print queue (wingdi) or CUPS queue (cups)
  -backend NAME     wingdi | cups | file | null
                    (default: wingdi on Windows; cups when CUPS queues
                    exist, else file elsewhere; file requires -out DIR)
  -out DIR          output dir for the file backend (default ./dyj-out)
  -spool DIR        state dir (default per-OS; also holds logs and tokens)
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

Data-plane auth (v0.3): loopback is exempt; LAN peers must pair once with
the 4-digit code shown on the share machine's admin page / tray.
`)
}
