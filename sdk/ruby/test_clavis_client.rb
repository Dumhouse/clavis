# frozen_string_literal: true

require "minitest/autorun"
require "socket"
require_relative "clavis_client"

# A tiny HTTP server built on TCPServer, so the tests need no gems beyond
# minitest. It records each request and answers with a fixed body.
class FakeServer
  attr_reader :requests

  def initialize(body: '{"secrets":[]}', status: 200, reason: "OK")
    @server = TCPServer.new("127.0.0.1", 0)
    @requests = []
    @thread = Thread.new do
      loop do
        handle(@server.accept, status, reason, body)
      end
    rescue IOError, Errno::EBADF
      nil
    end
  end

  def url
    "http://127.0.0.1:#{@server.addr[1]}"
  end

  def stop
    @server.close
    @thread.kill
    @thread.join
  end

  private

  def handle(socket, status, reason, body)
    line = socket.gets.to_s.strip
    headers = {}
    while (header = socket.gets)
      break if header == "\r\n"

      name, value = header.split(":", 2)
      headers[name.downcase] = value.strip if value
    end
    @requests << { line: line, headers: headers }
    socket.write("HTTP/1.1 #{status} #{reason}\r\nContent-Type: application/json\r\n" \
                 "Content-Length: #{body.bytesize}\r\nConnection: close\r\n\r\n#{body}")
  ensure
    socket.close
  end
end

class ClavisClientTest < Minitest::Test
  def setup
    @servers = []
    @saved_env = {}
  end

  def teardown
    @servers.each(&:stop)
    @saved_env.each do |key, value|
      value.nil? ? ENV.delete(key) : ENV[key] = value
    end
  end

  def server(**options)
    fake = FakeServer.new(**options)
    @servers << fake
    fake
  end

  def with_env(values)
    values.each do |key, value|
      @saved_env[key] = ENV[key] unless @saved_env.key?(key)
      value.nil? ? ENV.delete(key) : ENV[key] = value
    end
    yield
  end

  def clear_settings
    with_env("CLAVIS_SERVER" => nil, "CLAVIS_TOKEN" => nil, "CLAVIS_ORG" => nil,
             "CLAVIS_PROJECT" => nil, "CLAVIS_ENV" => nil) { yield }
  end

  def test_fetches_secrets_with_the_bearer_token
    fake = server(body: '{"secrets":[{"key":"RUBY_A","value":"1"},{"key":"RUBY_B","value":"two"}]}')
    secrets = clear_settings do
      Clavis.fetch_secrets(server: fake.url, token: "good", org: "acme", project: "web", env: "dev")
    end
    assert_equal({ "RUBY_A" => "1", "RUBY_B" => "two" }, secrets)
    request = fake.requests.first
    assert_equal "GET /api/v1/orgs/acme/projects/web/environments/dev/secrets:resolve?channel=api HTTP/1.1",
                 request[:line]
    assert_equal "Bearer good", request[:headers]["authorization"]
    assert_equal "clavis-ruby-sdk", request[:headers]["user-agent"]
  end

  def test_encodes_path_segments_and_defaults_to_dev
    fake = server
    clear_settings do
      Clavis.fetch_secrets(server: "#{fake.url}/", token: "good", org: "acme co", project: "web/app")
    end
    assert_equal "GET /api/v1/orgs/acme%20co/projects/web%2Fapp/environments/dev/secrets:resolve?channel=api HTTP/1.1",
                 fake.requests.first[:line]
  end

  def test_reads_settings_from_the_environment
    fake = server
    with_env("CLAVIS_SERVER" => fake.url, "CLAVIS_TOKEN" => "good", "CLAVIS_ORG" => "acme",
             "CLAVIS_PROJECT" => "web", "CLAVIS_ENV" => "production") do
      Clavis.fetch_secrets
    end
    assert_equal "GET /api/v1/orgs/acme/projects/web/environments/production/secrets:resolve?channel=api HTTP/1.1",
                 fake.requests.first[:line]
  end

  def test_keeps_empty_and_null_values
    fake = server(body: '{"secrets":[{"key":"EMPTY","value":""},{"key":"NULL","value":null}]}')
    secrets = clear_settings { Clavis.fetch_secrets(server: fake.url, token: "good", org: "a", project: "b") }
    assert_equal({ "EMPTY" => "", "NULL" => "" }, secrets)
  end

  def test_missing_settings_raise
    error = clear_settings do
      assert_raises(Clavis::Error) { Clavis.fetch_secrets(server: "http://127.0.0.1:1") }
    end
    assert_equal "missing token (pass it or set CLAVIS_TOKEN)", error.message
  end

  def test_http_errors_raise
    fake = server(status: 403, reason: "Forbidden")
    error = clear_settings do
      assert_raises(Clavis::Error) { Clavis.fetch_secrets(server: fake.url, token: "bad", org: "a", project: "b") }
    end
    assert_equal "the server answered 403 Forbidden", error.message
  end

  def test_unreachable_server_raises
    closed = TCPServer.new("127.0.0.1", 0)
    port = closed.addr[1]
    closed.close
    error = clear_settings do
      assert_raises(Clavis::Error) { Clavis.fetch_secrets(server: "http://127.0.0.1:#{port}", token: "good", org: "a", project: "b") }
    end
    assert_match(/could not reach the server/, error.message)
  end

  def test_load_secrets_keeps_values_already_set
    fake = server(body: '{"secrets":[{"key":"RUBY_KEEP","value":"from-server"},{"key":"RUBY_ADD","value":"two"}]}')
    with_env("RUBY_KEEP" => "original", "RUBY_ADD" => nil) do
      names = clear_settings { Clavis.load_secrets(server: fake.url, token: "good", org: "a", project: "b") }
      assert_equal ["RUBY_ADD"], names
      assert_equal "original", ENV["RUBY_KEEP"]
      assert_equal "two", ENV["RUBY_ADD"]
    end
  end

  def test_load_secrets_can_override
    fake = server(body: '{"secrets":[{"key":"RUBY_KEEP","value":"from-server"}]}')
    with_env("RUBY_KEEP" => "original") do
      names = clear_settings do
        Clavis.load_secrets(override: true, server: fake.url, token: "good", org: "a", project: "b")
      end
      assert_equal ["RUBY_KEEP"], names
      assert_equal "from-server", ENV["RUBY_KEEP"]
    end
  end
end
