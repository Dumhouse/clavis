# frozen_string_literal: true

# A tiny Clavis client for Ruby 3.1+ (standard library only).
#
# Fetch one environment's secrets at start-up, so an app does not need the CLI:
#
#   require_relative "clavis_client"
#   Clavis.load_secrets   # fills ENV from CLAVIS_* settings
#
# Settings come from the arguments or from CLAVIS_SERVER, CLAVIS_TOKEN,
# CLAVIS_ORG, CLAVIS_PROJECT and CLAVIS_ENV. The token needs the
# secrets:read-values permission for the environment. Reads are audited.

require "json"
require "net/http"
require "uri"

module Clavis
  # The server could not be reached, or refused the request.
  class Error < StandardError; end

  module_function

  # Return an environment's secrets as a Hash of NAME => value.
  def fetch_secrets(server: nil, token: nil, org: nil, project: nil, env: nil, timeout: 10)
    settings = {
      server: trim_slashes(server || ENV["CLAVIS_SERVER"] || ""),
      token: token || ENV["CLAVIS_TOKEN"],
      org: org || ENV["CLAVIS_ORG"],
      project: project || ENV["CLAVIS_PROJECT"]
    }
    settings.each do |name, value|
      raise Error, "missing #{name} (pass it or set CLAVIS_#{name.to_s.upcase})" if value.nil? || value.empty?
    end
    environment = env || ENV["CLAVIS_ENV"] || "dev"
    path = [settings[:org], settings[:project], environment].map { |part| URI.encode_uri_component(part) }
    url = "#{settings[:server]}/api/v1/orgs/#{path[0]}/projects/#{path[1]}" \
          "/environments/#{path[2]}/secrets:resolve?channel=api"
    body = get_json(url, settings[:token], timeout)
    (body["secrets"] || []).to_h { |secret| [secret["key"], secret["value"] || ""] }
  end

  # Fetch the secrets and put them in ENV.
  #
  # Values already set in the environment win unless +override+ is true.
  # Returns the names that were set.
  def load_secrets(override: false, **options)
    names = []
    fetch_secrets(**options).each do |key, value|
      next if !override && ENV.key?(key)

      ENV[key] = value
      names << key
    end
    names
  end

  # Drop trailing slashes without a regular expression, which could be slow on
  # a value with many of them.
  def trim_slashes(value)
    finish = value.length
    finish -= 1 while finish.positive? && value[finish - 1] == "/"
    value[0, finish]
  end

  def get_json(url, token, timeout)
    response =
      begin
        uri = URI.parse(url)
        request = Net::HTTP::Get.new(uri)
        request["Authorization"] = "Bearer #{token}"
        request["User-Agent"] = "clavis-ruby-sdk"
        Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https",
                                            open_timeout: timeout, read_timeout: timeout,
                                            write_timeout: timeout) do |http|
          http.request(request)
        end
      rescue StandardError => e
        raise Error, "could not reach the server: #{e.message}"
      end
    unless response.is_a?(Net::HTTPSuccess)
      raise Error, "the server answered #{response.code} #{response.message}"
    end

    JSON.parse(response.body)
  end

  private_class_method :get_json, :trim_slashes
end
