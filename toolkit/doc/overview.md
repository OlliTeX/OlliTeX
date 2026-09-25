# OlliTeX Toolkit Overview


## What is OlliTeX?

OlliTeX is a self-hostable, collaborative scientific writing and
publishing tool built on the Overleaf Community Edition codebase
(AGPL-3.0). It provides an easy-to-use LaTeX editor with real-time
collaboration and compiled output produced automatically as you type —
plus, in this distribution:

- a unified **/hub** workspace and admin UI,
- **Typst** alongside LaTeX (pinned, offline-safe compile image),
- **LanguageTool** grammar checking (optional service),
- LLM features and the module set (Zotero, WebDAV, GitHub, …).


## What is the OlliTeX Toolkit?

The [OlliTeX Toolkit](https://github.com/overleaf/toolkit) is a set of tools that allow anyone to run their own local version of Overleaf. The Overleaf software is distributed in [Docker](https://www.docker.com) images, while the toolkit manages the complexity of making those images run on your computer.


## Editions

This toolkit targets the OlliTeX **CE-based** build (the
`sharelatex/sharelatex:ext-6.3.0-port` image by default, or any
compatible build you name in `lib/images.env`). There is no commercial
edition in this distribution; everything you need — admin UI,
sandboxed compiles, LDAP/SAML, grammar checking — is included.

Image versions live in ONE file: `lib/images.env`.


## Docker, Docker Compose, and Overleaf

The toolkit uses [Docker](https://www.docker.com) and [Docker Compose](https://docs.docker.com/compose/) to run the Overleaf software in an isolated sandbox. While we do recommend becoming familiar with both Docker and Docker Compose, we also aim to make it as easy as possible to run Overleaf on your own computer.


## How do I get the Toolkit?

The toolkit ships inside the OlliTeX repository (this directory, `tools/toolkit`).

If you want to get started right now, we recommend you take a look at the
[Quick-Start Guide](./quick-start-guide.md).


## Toolkit Structure

If you take a look at the toolkit repository, you will see a file structure like this:

```
    bin/
    config/
    data/
    doc/
    lib/
    README.md
```

The `README.md` file contains some important information about the project. The `lib/` directory contains files that are internal to the toolkit, and users should not need to worry about. 


### Data Files

By default, the toolkit will put your overleaf data in the `data/` directory. This directory is ignored by git, so you don't need to worry about it being over-written by an update to the toolkit code.


### Configuration Files

Your own configuration files will live in the `config/` directory. This directory is also ignored by git, so it won't be over-written by the toolkit.


### The `bin/` directory

The `bin/` directory contains a collection of scripts, which will be your main interface to the toolkit system. We can start the Overleaf system with `bin/start`, we can check the logs with `bin/logs`, and we can back up our configuration with `bin/backup-config`


### Documentation

You will find all the documentation you need in the `doc/` directory. This documentation can also be viewed online, here: https://github.com/overleaf/toolkit/tree/master/doc/
