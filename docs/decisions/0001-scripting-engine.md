---
# These are optional elements. Feel free to remove any of them.
status: "{proposed | rejected | accepted | deprecated | … | superseded by [ADR-0005](0005-example.md)}"
date: {YYYY-MM-DD when the decision was last updated}
deciders: {list everyone involved in the decision}
consulted: {list everyone whose opinions are sought (typically subject-matter experts); and with whom there is a two-way communication}
informed: {list everyone who is kept up-to-date on progress; and with whom there is a one-way communication}
---
# {short title of solved problem and solution}

## Context and Problem Statement

We want the ability to provide custom providers that don't live inside this main repository. This allows groups that utilize this project to implement tile layers that involve very domain specific logic. For example, many companies provide map layers as part of their product offering that is integrated behind their own authentication methodology, often this doesn't follow any standard scheme. Another use case is integrating with inhouse software systems that don't provide an HTTP API.

<!-- This is an optional element. Feel free to remove. -->
## Decision Drivers

* {decision driver 1, e.g., a force, facing concern, …}
* {decision driver 2, e.g., a force, facing concern, …}
* … <!-- numbers of drivers can vary -->

## Considered Options

* Go - precompilation only
* Go - scripting solution
* Lua - scripting solution
* Python - scripting solution
* Javascript - scripting solution

## Decision Outcome

Chosen option: "{title of option 1}", because
{justification. e.g., only option, which meets k.o. criterion decision driver | which resolves force {force} | … | comes out best (see below)}.

<!-- This is an optional element. Feel free to remove. -->
### Consequences

* Good, because {positive consequence, e.g., improvement of one or more desired qualities, …}
* Bad, because {negative consequence, e.g., compromising one or more desired qualities, …}
* … <!-- numbers of consequences can vary -->

<!-- This is an optional element. Feel free to remove. -->
### Confirmation

{Describe how the implementation of/compliance with the ADR is confirmed. E.g., by a review or an ArchUnit test.
 Although we classify this element as optional, it is included in most ADRs.}

<!-- This is an optional element. Feel free to remove. -->
## Pros and Cons of the Options

### Go - precompilation only 

The null option, don't support anything beyond the core interface used for built-in providers.  Instead focus on making it as easy
as possible to compile this software so if a group needs to add a custom provider, they write it as a native provider in their own
fork of this repo.

* Pro: Easiest option, no extra coding required
* Con: Users of this software need to maintain their own forks and build processes
* Con: Changes to the core provider interface will break things for users

### Go - scripting solution

{example | description | pointer to more information | …}

* Pro: follows the pattern of traefik, which is a well known and similar tool
* Con: Go is less accessible than other options
* Con: 

### Lua - scripting solution

{example | description | pointer to more information | …}

* Pro: Lua is very popular for providing scripting/plugin functionality
* Con: 

### Python - scripting solution



* Pro: Very well-known language
* Pro: Can provide easiest transition path for custom providers written for tilestache
* Neutral, because {argument c}
* Bad: Python can be environmentally temperamental. Requiring cpython complicates installation/container maintenance
* Bad: Tools to support go/python interop mostly either aren't mature or aren't well maintained


### Javascript - scripting solution

{example | description | pointer to more information | …}

* Good, because {argument a}
* Good, because {argument b}
* Neutral, because {argument c}
* Bad, because {argument d}
* …

<!-- This is an optional element. Feel free to remove. -->
## More Information

{You might want to provide additional evidence/confidence for the decision outcome here and/or
 document the team agreement on the decision and/or
 define when/how this decision the decision should be realized and if/when it should be re-visited.
Links to other decisions and resources might appear here as well.}
