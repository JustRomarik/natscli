// Copyright 2020 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	"gopkg.in/alecthomas/kingpin.v2"
)

type subCmd struct {
	subject string
	queue   string
	raw     bool
	jsAck   bool
	inbox   bool
}

func configureSubCommand(app *kingpin.Application) {
	c := &subCmd{}
	act := app.Command("sub", "Generic subscription client").Action(c.subscribe)
	act.Arg("subject", "Subject to subscribe to").StringVar(&c.subject)
	act.Flag("queue", "Subscribe to a named queue group").StringVar(&c.queue)
	act.Flag("raw", "Show the raw data received").Short('r').BoolVar(&c.raw)
	act.Flag("ack", "Acknowledge JetStream message that have the correct metadata").BoolVar(&c.jsAck)
	act.Flag("inbox", "Subscribes to a generate inbox").Short('i').BoolVar(&c.inbox)

	cheats["sub"] = `# To subscribe to messages, in a queue group and acknowledge any JetStream ones
nats sub source.subject --queue work --ack

# To subscribe to a randomly generated inbox
nats sub --inbox
`
}

func (c *subCmd) subscribe(_ *kingpin.ParseContext) error {
	if c.subject == "" && c.inbox {
		c.subject = nats.NewInbox()
	} else if c.subject == "" {
		return fmt.Errorf("subject is required")
	}

	nc, err := newNatsConn("", natsOpts()...)
	if err != nil {
		return err
	}
	defer nc.Close()

	i := 0
	mu := sync.Mutex{}

	handler := func(m *nats.Msg) {
		mu.Lock()
		defer mu.Unlock()

		i += 1

		var info *jsm.MsgInfo
		if m.Reply != "" {
			info, _ = jsm.ParseJSMsgMetadata(m)
		}

		if c.jsAck && info != nil {
			defer func() {
				err = m.Respond(nil)
				if err != nil {
					log.Printf("Acknowledging message via subject %s failed: %s\n", m.Reply, err)
				}

			}()
		}

		if c.raw {
			fmt.Println(string(m.Data))
			return
		}

		if info == nil {
			if m.Reply != "" {
				fmt.Printf("[#%d] Received on %q with reply %q\n", i, m.Subject, m.Reply)
			} else {
				fmt.Printf("[#%d] Received on %q\n", i, m.Subject)
			}

		} else {
			fmt.Printf("[#%d] Received JetStream message: consumer: %s > %s / subject: %s / delivered: %d / consumer seq: %d / stream seq: %d / ack: %v\n", i, info.Stream(), info.Consumer(), m.Subject, info.Delivered(), info.ConsumerSequence(), info.StreamSequence(), c.jsAck)
		}

		if len(m.Header) > 0 {
			for h, vals := range m.Header {
				for _, val := range vals {
					fmt.Printf("%s: %s\n", h, val)
				}
			}

			fmt.Println()
		}

		fmt.Println(string(m.Data))
		if !strings.HasSuffix(string(m.Data), "\n") {
			fmt.Println()
		}
	}

	if !c.raw || c.inbox {
		if c.jsAck {
			log.Printf("Subscribing on %s with acknowledgement of JetStream messages\n", c.subject)
		} else {
			log.Printf("Subscribing on %s\n", c.subject)
		}
	}

	if c.queue != "" {
		nc.QueueSubscribe(c.subject, c.queue, handler)
	} else {
		nc.Subscribe(c.subject, handler)
	}
	nc.Flush()

	err = nc.LastError()
	if err != nil {
		return err
	}

	<-context.Background().Done()

	return nil
}
