# Reporting a security bug

All security bugs in Sinatra should be reported to the core team through our private mailing list [sinatra-security@googlegroups.com](https://groups.google.com/group/sinatra-security). Your report will be acknowledged within 24 hours, and you’ll receive a more detailed response to your email within 48 hours indicating the next steps in handling your report.

After the initial reply to your report the security team will endeavor to keep you informed of the progress being made towards a fix and full announcement. These updates will be sent at least every five days, in reality this is more likely to be every 24-48 hours.

If you have not received a reply to your email within 48 hours, or have not heard from the security team for the past five days there are a few steps you can take:

* Contact the current security coordinator [Zachary Scott](mailto:zzak@ruby-lang.org) directly

## Disclosure Policy

Sinatra has a 5 step disclosure policy, that is upheld to the best of our ability.

1. Security report received and is assigned a primary handler. This person will coordinate the fix and release process.
2. Problem is confirmed and, a list of all affected versions is determined. Code is audited to find any potential similar problems.
3. Fixes are prepared for all releases which are still supported. These fixes are not committed to the public repository but rather held locally pending the announcement.
4. A suggested embargo date for this vulnerability is chosen and distros@openwall is notified. This notification will include patches for all versions still under support and a contact address for packagers who need advice back-porting patches to older versions.
5. On the embargo date, the [mailing list][mailing-list] and [security list][security-list] are sent a copy of the announcement. The changes are pushed to the public repository and new gems released to rubygems.

Typically the embargo date will be set 72 hours from the time vendor-sec is first notified, however this may vary depending on the severity of the bug or difficulty in applying a fix.

This process can take some time, especially when coordination is required with maintainers of other projects. Every effort will be made to handle the bug in as timely a manner as possible, however it’s important that we follow the release process above to ensure that the disclosure is handled in a consistent manner.

## Security Updates

Security updates will be posted on the [mailing list][mailing-list] and [security list][security-list].

## Comments on this Policy

If you have any suggestions to improve this policy, please send an email the core team at [sinatrarb@googlegroups.com](https://groups.google.com/group/sinatrarb).


[mailing-list]: http://groups.google.com/group/sinatrarb/topics
[security-list]: http://groups.google.com/group/sinatra-security/topics
