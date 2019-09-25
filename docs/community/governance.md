# Loki's Governance Model

This document describes the rules and governance of the Loki project. It is meant to be followed by all the developers of the project and the community.

## Values

The Loki [team](#team-members) and community are expected to follow the values defined in the [Loki Code of Conduct](https://github.com/grafana/loki/blob/master/CODE_OF_CONDUCT.md). Furthermore, the Loki community strives for kindness, giving feedback effectively, and building a welcoming environment. The Loki developers generally decide by lazy consensus and only resort to conflict resolution by a majority vote if consensus cannot be reached.

## New contributors

New contributors should be welcomed to the community by existing members, helped with PR workflow, and directed to relevant documentation and communication channels.

## Decision making

### Team members

Team member status may be given to those who have made ongoing contributions to the Loki project for at least 3 months. This is usually in the form of code improvements and/or notable work on documentation, but organizing events or user support would also be taken into account.

Established members are expected to demonstrate their adherence to the principles in this document, familiarity with project organization, roles, policies, procedures, conventions, etc., and technical and/or writing ability. Role-specific expectations, responsibilities, and requirements are enumerated below.

The current team members are:

- Cyril Tovena
- David Kaltschmidt
- Edward Welch
- Goutham Veeramachaneni
- Robert Fratto
- Sandeep Sukhani
- Tom Braack
- Tom Wilkie

### Responsibilities and privileges

- Responsible for project quality control via code reviews
  - Focus on code quality and correctness, including testing and factoring
  - May also review for more holistic issues, but not a requirement
- Expected to be responsive to review requests in a timely manner
- Assigned PRs to review related based on expertise
- Granted commit access to Loki repository

### New members

New members may be proposed by any existing member by email to loki-team@googlegroups.com. It is highly desirable to reach consensus about acceptance of a new member. However, the proposal is ultimately voted on by a formal [supermajority vote](#supermajority-vote).

If the new member proposal is accepted, the proposed team member should be contacted privately via email to confirm or deny their acceptance of team membership. This email will also be CC'd to loki-team@googlegroups.com for record-keeping purposes.

If they choose to accept, the following steps are taken:

- Team members are added to the [GitHub project](https://github.com/grafana/loki) as Collaborator (Push access to the repository).
- **Team member must enable [two-factor authentication](https://help.github.com/articles/about-two-factor-authentication) on their GitHub account**
- Team members are added to the [team mailing list](mailto:loki-team@googlegroups.com).
- Team members are added to the list of team members in this document.
- New team members are announced on the [discussion group](https://groups.google.com/forum/#!forum/lokiproject) and Loki [slack channel](https://grafana.slack.com/messages/CEPJRLQNL) by an existing team member.

Team members may retire at any time by emailing the [team](mailto:loki-team@googlegroups.com).

Team members can be removed by [supermajority vote](#supermajority-vote) on the team [mailing list](mailto:loki-team@googlegroups.com). For this vote, the member in question is not eligible to vote and does not count towards the quorum.

Upon death of a member, their team membership ends automatically.

### Technical decisions

Technical decisions are made informally by the team member, and [lazy consensus](#consensus) is assumed. If no consensus can be reached, the matter may be resolved by [majority vote](#majority-vote).

### Governance changes

Material changes to this document are discussed publicly on as an issue on the Loki repository. Any change requires a [supermajority](#supermajority-vote) in favor. Editorial changes may be made by [lazy consensus](#consensus) unless challenged.

### Other matters

Any matter that needs a decision, including but not limited to financial matters, may be called to a vote by any member if they deem it necessary. For financial, private, or personnel matters, discussion and voting takes place on the [team mailing list](mailto:loki-team@googlegroups.com), otherwise on the public [mailing list](https://groups.google.com/forum/#!forum/lokiproject).

## Voting

The Loki project usually runs by informal consensus, however sometimes a formal decision must be made.

Depending on the subject matter, as laid out [above](#decision-making), different methods of voting are used.

For all votes, voting must be open for at least one week. The end date should be clearly stated in the call to vote. A vote may be called and closed early if enough votes have come in one way so that further votes cannot change the final decision.

In all cases, all and only [team members](#team-members) are eligible to vote, with the sole exception of the forced removal of a team member, in which said member is not eligible to vote.

Discussion and votes on personnel matters (including but not limited to team membership and maintainership) are held in private on the [team mailing list](mailto:loki-team@googlegroups.com). All other discussion and votes are held in the public [mailing list](https://groups.google.com/forum/#!forum/lokiproject) or [slack](https://grafana.slack.com/messages/CEPJRLQNL).

For public discussions, anyone interested is encouraged to participate. Formal power to object or vote is limited to [team members](#team-members).

## Consensus

The default decision making mechanism for the Loki project is [lazy consensus](https://couchdb.apache.org/bylaws.html#lazy). This means that any decision on technical issues is considered supported by the [team](#team-members) as long as nobody objects.

Silence on any consensus decision is implicit agreement and equivalent to explicit agreement. Explicit agreement may be stated at will. Decisions may, but do not need to be called out and put up for decision publicly at any time and by anyone.

Consensus decisions can never override or go against the spirit of an earlier explicit vote.

If any [team member](#team-members) raises objections, the team members work together towards a solution that all involved can accept. This solution is again subject to lazy consensus.

In case no consensus can be found, but a decision one way or the other must be made, any [team member](#team-members) may call a formal [majority vote](#majority-vote).

## Majority vote

Majority votes must be called explicitly in a separate thread on the appropriate mailing list. The subject must be prefixed with [VOTE]. In the body, the call to vote must state the proposal being voted on. It should reference any discussion leading up to this point.

Votes may take the form of a single proposal, with the option to vote yes or no, or the form of multiple alternatives.

A vote on a single proposal is considered successful if more vote in favor than against.
If there are multiple alternatives, members may vote for one or more alternatives, or vote “no” to object to all alternatives. It is not possible to cast an “abstain” vote. A vote on multiple alternatives is considered decided in favor of one alternative if it has received the most votes in favor, and a vote from more than half of those voting. Should no alternative reach this quorum, another vote on a reduced number of options may be called separately.

## Supermajority vote

Supermajority votes must be called explicitly in a separate thread on the appropriate mailing list. The subject must be prefixed with [VOTE]. In the body, the call to vote must state the proposal being voted on. It should reference any discussion leading up to this point.

Votes may take the form of a single proposal, with the option to vote yes or no, or the form of multiple alternatives.

A vote on a single proposal is considered successful if at least two thirds of those eligible to vote vote in favor.

If there are multiple alternatives, members may vote for one or more alternatives, or vote “no” to object to all alternatives. A vote on multiple alternatives is considered decided in favor of one alternative if it has received the most votes in favor, and a vote from at least two thirds of those eligible to vote. Should no alternative reach this quorum, another vote on a reduced number of options may be called separately.
