# Commit privacy

Enable the repository hooks after cloning and use the noreply email shown in your GitHub email settings. Set your Git name to your public contributor name.

```sh
git config --local core.hooksPath .githooks
git config --local user.name "PUBLIC-ALIAS"
git config --local user.email "YOUR-GITHUB-NOREPLY-ADDRESS"
```

The hooks require Python 3.8 or later. They check the Git author, committer, message, and newly staged private data. The checker reads the index and compares it with the previous version, so an unrelated edit can be committed without rewriting legacy examples. It checks notebook text and metadata, and the creator and last editor of Word, Excel, and PowerPoint files.

Add project-specific private names or identifiers to the local file returned by `git rev-parse --git-path info/privacy-identifiers`, one literal per line. Reports name the file and category without printing matches. Review document bodies, images, archives, and embedded media separately. Office inspection is limited to 100 MiB packages and 256 KiB metadata parts.

Run the isolated Git integration tests with `python3 -m unittest discover -s tests -p 'test_commit_privacy.py'`. The hooks apply to commits made in this checkout. Web edits and other clones use their own settings.
