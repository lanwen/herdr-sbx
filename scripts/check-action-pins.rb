#!/usr/bin/env ruby
# Read only: use Psych's YAML syntax tree so quotes, inline mappings, and comments
# cannot conceal mutable references. No external YAML gem is needed.
require 'open3'
begin
  require 'psych'
rescue LoadError
  warn 'Action pin checking requires Ruby with the standard-library Psych YAML parser.'
  exit 1
end

if ARGV == ['--help']
  puts 'Usage: ruby scripts/check-action-pins.rb [workflow-or-action.yml ...]'
  puts 'Requires Ruby/Psych. Defaults to workflow YAML and tracked action metadata.'
  exit
end

def mapping(node)
  raise 'expected a YAML mapping' unless node.is_a?(Psych::Nodes::Mapping)
  node.children.each_slice(2).each_with_object({}) do |(key, value), result|
    raise 'mapping keys must be literal strings' unless key.is_a?(Psych::Nodes::Scalar)
    raise "duplicate YAML key #{key.value.inspect}" if result.key?(key.value)
    result[key.value] = value
  end
end

def literal(node)
  raise 'uses must be a literal YAML string' unless node.is_a?(Psych::Nodes::Scalar)
  node.value
end

def check_reference(node, path)
  value = literal(node)
  valid = if value.start_with?('./')
            true
          elsif value.start_with?('docker://')
            value.match?(/\Adocker:\/\/[^\s@]+@sha256:[0-9a-fA-F]{64}\z/)
          else
            value.match?(/\A[\w.-]+\/[\w.-]+(?:\/[\w.\/-]+)?@[0-9a-fA-F]{40}\z/)
          end
  raise "#{path}:#{node.start_line + 1}: uses #{value.inspect} must pin a full commit SHA (or Docker sha256 digest)" unless valid
end

def check_steps(node, path)
  raise 'steps must be a literal YAML sequence' unless node.is_a?(Psych::Nodes::Sequence)
  node.children.each do |step|
    fields = mapping(step)
    check_reference(fields['uses'], path) if fields.key?('uses')
  end
end

def reject_indirection(node)
  if node.is_a?(Psych::Nodes::Alias) || (node.respond_to?(:anchor) && node.anchor) || (node.respond_to?(:tag) && node.tag)
    raise 'YAML aliases, anchors, and explicit tags are unsupported; write literal action references'
  end
  (node.children || []).each { |child| reject_indirection(child) }
end

def check_file(path)
  stream = Psych.parse_stream(File.read(path))
  raise 'expected exactly one YAML document' unless stream.children.length == 1
  reject_indirection(stream)
  fields = mapping(stream.children.first.root)
  if fields.key?('jobs')
    mapping(fields['jobs']).each_value do |job|
      fields = mapping(job)
      check_reference(fields['uses'], path) if fields.key?('uses')
      check_steps(fields['steps'], path) if fields.key?('steps')
    end
  elsif fields.key?('runs')
    runs = mapping(fields['runs'])
    check_steps(runs['steps'], path) if runs['using'] && literal(runs['using']) == 'composite'
  else
    raise 'expected workflow jobs or action runs metadata'
  end
end

begin
  paths = ARGV
  if paths.empty?
    root = File.expand_path('..', __dir__)
    Dir.chdir(root)
    tracked, status = Open3.capture2('git', 'ls-files', '-z', '--', '**/action.yml', '**/action.yaml', 'action.yml', 'action.yaml')
    raise 'could not enumerate tracked action metadata' unless status.success?
    paths = (Dir.glob('.github/workflows/*.{yml,yaml}') + tracked.split("\0")).uniq
  end
  raise 'no workflow or action YAML files found' if paths.empty?
  paths.each { |path| check_file(path) }
  puts "Checked #{paths.length} YAML files; remote action references are immutable."
rescue Psych::Exception, IOError, SystemCallError, RuntimeError => error
  warn "Action pin check failed: #{error.message}"
  exit 1
end
