# Vanadium Peer to Peer Messaging

This package contains an experimental prototype for peer-to-peer messaging
where there might not be a direct network connection between the sender and the
receiver. Messages are relayed from peer to peer, and can tolerate long delays.

There are many mobile devices, especially in developing countries, that don't
have a reliable connection with cloud service providers to exchange messages
with each other and with cloud services. These devices often come in contact
with other devices that might be better connected and could be used to carry
messages for them.

The architecture is inspired by [RFC 4838] (Delay-Tolerant Networking
Architecture).


![Basic Message Diagram](diagrams/basic.png?raw=true)

The Sender of the message gives it to a _Messenger Node_. The _Messenger Node_
will store the message until it meets other nodes that can move the message
towards the Recipient.

This prototype uses the Vanadium [Discovery API] to find nearby and global
peers, and its own [Messenger Interface] to share messages.

Messages are signed and can be validated offline using the [Vanadium Security].

The content of each message is opaque to everyone except the intended recipient.
It is the sender's responsibility to encrypt the message content.

## Access Control

Access control is done with the [Vanadium Security] model. Each node can
authorize messages based on the identity of the peer that is relaying it, or
the identity of the original sender.

In addition, nodes can apply restrictions on the message size, lifespan, the
number of nodes it has already been through, etc.

## Message Routing

This implementation attempts to send all the messages to all the _trusted_ peers
that it encounters.

A smarter implementation will require better routing decisions. We will need a
way to know approximately where the recipient is and only send the message to
peers that allow it to make progress in the right direction.

There are still some potentially useful applications of the simple
send-everywhere model.

## Possible Applications

In general, the number of nodes that implement the [Messenger Interface] has to
be small. Sharing an unbounded number of messages with thousands or millions of
Messenger nodes isn't a realistic objective.

### Group of friends who want to exchange text messages, photos, etc.

![Campfire Diagram](diagrams/campfire.png?raw=true)

Each member of the group implements the [Messenger Interface], and access to
each method is restricted to the members of the group.

Messages can be addressed to individual members, or the whole group, but
everyone in the group can potentially end up with a copy of all messages.

### Isolated large group of people with a small number of data mules

![Island Diagram](diagrams/island.png?raw=true)

The [data mules] implement the [Messenger Interface] and optionally the
MessageRepository interface, and everyone in the group pulls the data they want
from them.

The data mules share the messages with the other data mules. Occasionally, the
data mules come in contact with cloud services to deliver outbound messages and
receive inbound messages for the group.

In this model, the data mules are similar to mobile email servers. The members
of the group don't share anything directly with the other members of the group.
Instead, all messages are exchanged via the data mules.

The data mules control who can access the service they provide.

### Content distribution

![Distribution Diagram](diagrams/distribution.png?raw=true)

In this model, everyone who is interested in the content implements the
[Messenger Interface] and authorizes Push requests using the message's
SenderBlessings so that only content from trusted sources will be accepted. Each
node can opportunistically push a copy of the messages to other nodes as long as
the other nodes trust the original sender of the message.

### Data and log collection

![Collection Diagram](diagrams/collection.png?raw=true)

When multiple devices want to send data to a central collection server, we need
peers that implement the [Messenger Interface] and know how to relay message to
the central collection server. These messages can be authorized using a
combination of SenderBlessings and recipient.

## Prototype implementation

The Messenger Server has 3 main components:

   * The Messenger Service
   * The Messenger Storage
   * The Peer Manager

### Messenger Service

The Messenger Service implements the [Messenger Interface] to receive incoming
messages of arbitrary sizes. The content of the messages is streamed from the
client to the server. If the stream is interrupted, it can be resumed at a later
time.

The message and its content are stored in the Messenger Storage.

When a new message is completely received, a notification is sent to the Peer
Manager, and the message is added to all the existing peers' message queues.

### The Messenger Storage

The Messenger Storage uses the local file system to store the messages and to
keep track of where the messages have been sent. Messages become visible for
sharing after they are fully copied.

### The Peer Manager

The Peer Manager uses the [Discovery API] to find peers with whom to
communicate. When a new peer is found, all the existing messages are added to
the send queue of this peer. Messages are removed from the queue after being
successfully transmitted or when a fatal error occurs. If a transient error
occurs, the message is added back to the queue to be retransmitted later.

There is a limit to how many peers a node will talk to. When there are too
many peers, we use a peer selection algorithm that makes it likely that all
the peers will eventually see all the messages.

For a peer limit L, the selected peers are the node's neighbors at distances
2^n, with n=[0..L].

![Ring Diagram](diagrams/ring.png?raw=true)

In this diagram, each node talks to 4 peers. The first one is the first (2^0)
neighbor. The second one is the second (2^1) neighbor. The third one is the
fourth (2^2) neighbor, etc. So, Node A sends the message to Nodes B, C, E, and
I.

After Node B received the message, it will send it to Nodes C, D, F, and J. Node
C will send it to Nodes D, E, G, and K, etc. This continues until all nodes have
a copy of the message.

## Security

Incoming and outgoing messages are controlled by rate limiting access lists
(RateAcl). The access lists are applied to the identity of the peers, both
sending and receiving the messages, and to the original sender of the message.

## Demo

The demo commandline application in called [vmsg](vmsg/doc.go).

It runs a Messenger Node to exchange text messages and files with other
Messenger Nodes.

```
# Create credentials
jiri go install v.io/x/ref/cmd/principal
$JIRI_ROOT/release/go/bin/principal create $HOME/.vmsg-creds
$JIRI_ROOT/release/go/bin/principal --v23.credentials=$HOME/.vmsg-creds seekblessings

# Start chat app
jiri go install messenger/vmsg
$JIRI_ROOT/release/projects/go/bin/vmsg chat --v23.credentials=$HOME/.vmsg-creds --store-dir=/tmp/store
```

## Screenshot

![Screenshot](diagrams/screenshot.png?raw=true)

[RFC 4838]: https://tools.ietf.org/html/rfc4838
[Discovery API]: https://github.com/vanadium/go.v23/blob/master/discovery/model.go
[Messenger Interface]: ifc/service.vdl
[Vanadium Security]: https://vanadium.github.io/concepts/security.html
[data mules]: https://en.wikipedia.org/wiki/Data_mule

