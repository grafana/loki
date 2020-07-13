# RSpec::Wait

Wait for conditions in RSpec

[![Gem Version](https://img.shields.io/gem/v/rspec-wait.svg?style=flat-square)](https://rubygems.org/gems/rspec-wait)
[![Build Status](https://img.shields.io/travis/laserlemon/rspec-wait/master.svg?style=flat-square)](https://travis-ci.org/laserlemon/rspec-wait)
[![Code Climate](https://img.shields.io/codeclimate/github/laserlemon/rspec-wait.svg?style=flat-square)](https://codeclimate.com/github/laserlemon/rspec-wait)
[![Coverage Status](https://img.shields.io/codeclimate/coverage/github/laserlemon/rspec-wait.svg?style=flat-square)](https://codeclimate.com/github/laserlemon/rspec-wait)
[![Dependency Status](https://img.shields.io/gemnasium/laserlemon/rspec-wait.svg?style=flat-square)](https://gemnasium.com/laserlemon/rspec-wait)

## Why does RSpec::Wait exist?

Timing is hard.

Timing problems and race conditions can plague your test suite. As your test
suite slowly becomes less reliable, development speed and quality suffer.

RSpec::Wait strives to make it easier to test asynchronous or slow interactions.

## How does RSpec::Wait work?

RSpec::Wait allows you to wait for an assertion to pass, using the RSpec
syntactic sugar that you already know and love.

RSpec::Wait will keep trying until your assertion passes or times out.

### Example

RSpec::Wait's `wait_for` assertions are nearly drop-in replacements for RSpec's
`expect` assertions. The major difference is that the `wait_for` method only
works with non-block matchers. However, `wait_for` will still accept a block
because it may need to evaluate the content of that block multiple times while
waiting.

```ruby
describe Ticker do
  subject(:ticker) { Ticker.new("foo") }

  describe "#start" do
    before do
      ticker.start
    end

    it "starts a blank tape" do
      expect(ticker.tape).to eq("")
    end

    it "writes the message one letter at a time" do
      wait_for(ticker.tape).to eq("··-·")
      wait_for(ticker.tape).to eq("··-· ---")
      wait_for(ticker.tape).to eq("··-· --- ---")
    end
  end
end
```

This can be especially useful for testing user interfaces with tricky timing
elements like JavaScript interactions or remote requests.

```ruby
feature "User Login" do
  let!(:user) { create(:user, email: "john@example.com", password: "secret") }

  scenario "A user can log in successfully" do
    visit new_session_path

    fill_in "Email", with: "john@example.com"
    fill_in "Password", with: "secret"
    click_button "Log In"

    wait_for { current_path }.to eq(account_path)
    expect(page).to have_content("Welcome back!")
  end
end
```

### Matchers

RSpec::Wait ties into RSpec's internals so it can take full advantage of any
non-block matcher that you would use with RSpec's own `expect` method.

### Timeout

By default, RSpec::Wait will wait up to 10 seconds for an assertion to pass.
That timeout value is configurable in three ways:

#### RSpec Configuration

```ruby
RSpec.configure do |config|
  config.wait_timeout = 3 # seconds
end
```

#### RSpec Metadata

The timeout can also be specified via options added to a spec's or context's
`:wait` metadata:

```ruby
scenario "A user can log in successfully", wait: { timeout: 3 } do
  visit new_session_path

  fill_in "Email", with: "john@example.com"
  fill_in "Password", with: "secret"
  click_button "Log In"

  wait_for { current_path }.to eq(account_path)
  expect(page).to have_content("Welcome back!")
end
```

#### The `wait` Method

On a per-assertion basis, the timeout value can be passed to the `wait` method.

```ruby
scenario "A user can log in successfully" do
  visit new_session_path

  fill_in "Email", with: "john@example.com"
  fill_in "Password", with: "secret"
  click_button "Log In"

  wait(3.seconds).for { current_path }.to eq(account_path)
  expect(page).to have_content("Welcome back!")
end
```

### Use with Cucumber

To enable RSpec::Wait in your Cucumber step definitions, add the following to
`features/support/env.rb`:

```ruby
require "rspec/wait"

World(RSpec::Wait)
```

## Who wrote RSpec::Wait?

My name is Steve Richert and I wrote RSpec::Wait in April, 2014 with the support
of my employer, [Collective Idea](http://www.collectiveidea.com). RSpec::Wait
owes its current and future success entirely to [inspiration](https://github.com/laserlemon/rspec-wait/issues)
and [contribution](https://github.com/laserlemon/rspec-wait/graphs/contributors)
from the Ruby community, especially the [authors and maintainers](https://github.com/rspec/rspec-core/graphs/contributors)
of RSpec.

**Thank you!**

## How can I help?

RSpec::Wait is open source and contributions from the community are encouraged!
No contribution is too small.

See RSpec::Wait's [contribution guidelines](CONTRIBUTING.md) for more
information.
