# Catalogue entry

The entry for tikaydaes.dev that `BRIEF.md` asks for (deliverable 7),
written in the shape of the entries in that site's `products.mjs`. It is
not added there yet: Churr is not ready to be offered (`README.md`, "What
exists today"). The catch is the brief's, word for word.

- **SKU:** TD-005
- **Name:** Churr
- **Kind:** Self-hosted server + apps
- **Price:** $0.00
- **Pitch:** Team chat you run yourself, with everything in the box.
- **Description:** Channels, threads, files, search, polls, bots, calls
  and sealed two-person chats, for groups of 5 to 5,000. The server is
  one Go binary with its database inside, and people chat in apps for
  phone and desktop or in a web chat the server serves itself. Servers
  can share channels with each other, directly or through a hub. No
  charge per person and no feature held back.
- **Specs:**

  | | |
  | --- | --- |
  | Licence | AGPL-3.0 |
  | Needs | A machine with ports 80 and 443 open, or a reverse proxy |
  | Account | Passkeys, or company sign-in over OIDC or SAML. No passwords, no email |

- **The catch:** Whoever runs a Churr server can read everything on it,
  private channels included, except sealed chats, which are always
  between exactly two people. A shared channel can be read by the
  operator of every server it is shared with. A hub's operator cannot
  read it, but sees its name, its managers and moderators and which servers are in
  it, and decides which servers are let in. That is why Churr is built
  for a group to run for itself.

As an object for `products.mjs`:

```js
  {
    sku: "TD-005",
    name: "Churr",
    kind: "Self-hosted server + apps",
    pitch: "Team chat you run yourself, with everything in the box.",
    body: "Channels, threads, files, search, polls, bots, calls and sealed two-person chats, for groups of 5 to 5,000. The server is one Go binary with its database inside, and people chat in apps for phone and desktop or in a web chat the server serves itself. Servers can share channels with each other, directly or through a hub. No charge per person and no feature held back.",
    specs: [
      ["Licence", "AGPL-3.0"],
      ["Needs", "A machine with ports 80 and 443 open, or a reverse proxy"],
      ["Account", "Passkeys, or company sign-in over OIDC or SAML. No passwords, no email"],
    ],
    catch: "Whoever runs a Churr server can read everything on it, private channels included, except sealed chats, which are always between exactly two people. A shared channel can be read by the operator of every server it is shared with. A hub's operator cannot read it, but sees its name, its managers and moderators and which servers are in it, and decides which servers are let in. That is why Churr is built for a group to run for itself.",
    get: { label: "Get the source", href: gh("churr") },
    source: gh("churr"),
    art: { src: "p-churr-b", alt: "A nightjar in headphones sitting on a server box." },
  },
```
