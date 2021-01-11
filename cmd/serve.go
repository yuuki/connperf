/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve accepts connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		lport := cmd.Flags().Uint16P("port", "p", 9100, "listening port")
		return serve(*lport)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func serve(lport uint16) error {
	addr := fmt.Sprintf(":%d", lport)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %q error: %s", addr, err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok {
				if ne.Temporary() {
					log.Printf("temporary error when listening for TCP addr %q: %s", ln.Addr(), err)
					time.Sleep(time.Second)
					continue
				}
				if strings.Contains(err.Error(), "use of closed network connection") {
					break
				}
				log.Fatalf("unrecoverable error when accepting TCP connections: %s", err)
			}
			log.Fatalf("unexpected error when accepting TCP Graphite connections: %s", err)
		}
		go func() {
			if err := echoStream(conn); err != nil {
				log.Println(err)
				if err := conn.Close(); err != nil {
					log.Println("could not close: %s", err)
				}
			}
		}()
	}

	return nil
}

func echoStream(conn net.Conn) error {
	// if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
	// 	log.Printf("could not set read deadline: %s\n", err)
	// }
	r := bufio.NewReader(conn)
	msg, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Errorf("could not read: %s\n", err)
	}
	if _, err := conn.Write(msg); err != nil {
		return errors.Errorf("could write %q", msg)
	}
	return nil
}
