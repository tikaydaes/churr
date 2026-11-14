#!/usr/bin/env ruby
# Adds the iPhone notification service extension (ios/NotificationService,
# docs/PUSH.md section 8) to ios/Runner.xcodeproj, embeds it in the app,
# points both targets at their entitlements (the shared keychain group),
# and adds the app's own Localizable.strings ("WAKE_FALLBACK", the
# relay's loc-key) and the ring sound to the app's resources.
#
# Idempotent: run again, it changes nothing that is already there.
# Needs the `xcodeproj` gem, which CocoaPods carries:
#
#   GEM_HOME=$(dirname $(dirname $(readlink -f $(which pod))))/libexec \
#     ruby tool/ios_notification_extension.rb
#
# (with Homebrew's CocoaPods: GEM_HOME=/opt/homebrew/Cellar/cocoapods/<v>/libexec)

require 'xcodeproj'

ROOT = File.expand_path('../ios', __dir__)
project = Xcodeproj::Project.open(File.join(ROOT, 'Runner.xcodeproj'))
runner = project.targets.find { |t| t.name == 'Runner' }
NAME = 'NotificationService'
BUNDLE = 'example.notfinal.churr.NotificationService' # with the app's placeholder id

ext = project.targets.find { |t| t.name == NAME }
unless ext
  ext = project.new_target(:app_extension, NAME, :ios, '15.0', nil, :swift)
end

group = project.main_group.children.find { |g| g.display_name == NAME } ||
        project.main_group.new_group(NAME, NAME)

def file_ref(group, path)
  group.files.find { |f| f.path == path } || group.new_reference(path)
end

sources = ['NotificationService.swift'] +
          Dir.children(File.join(ROOT, NAME, 'Sources/ChurrNotify')).sort.select { |f| f.end_with?('.swift') }
                .map { |f| "Sources/ChurrNotify/#{f}" }
sources.each do |path|
  ref = file_ref(group, path)
  ext.source_build_phase.add_file_reference(ref, true)
end
file_ref(group, 'Info.plist')
file_ref(group, "#{NAME}.entitlements")

# The extension's wording.
strings = group.children.find { |c| c.display_name == 'Localizable.strings' }
unless strings
  strings = group.new_variant_group('Localizable.strings')
  strings.new_reference('en.lproj/Localizable.strings').name = 'en'
end
ext.resources_build_phase.add_file_reference(strings, true)

ext.build_configurations.each do |c|
  s = c.build_settings
  s['PRODUCT_BUNDLE_IDENTIFIER'] = BUNDLE
  s['PRODUCT_NAME'] = '$(TARGET_NAME)'
  s['INFOPLIST_FILE'] = "#{NAME}/Info.plist"
  s['CODE_SIGN_ENTITLEMENTS'] = "#{NAME}/#{NAME}.entitlements"
  s['SWIFT_VERSION'] = '5.0'
  s['IPHONEOS_DEPLOYMENT_TARGET'] = '15.0'
  s['TARGETED_DEVICE_FAMILY'] = '1,2'
  s['SKIP_INSTALL'] = 'YES'
  s['GENERATE_INFOPLIST_FILE'] = 'NO'
  s['CURRENT_PROJECT_VERSION'] = '1'
  s['MARKETING_VERSION'] = '0.1.0'
  # The app's version, as Flutter sets it for the Runner target.
  s['FLUTTER_BUILD_NAME'] = '0.1.0'
  s['FLUTTER_BUILD_NUMBER'] = '1'
  s['LD_RUNPATH_SEARCH_PATHS'] = ['$(inherited)', '@executable_path/Frameworks', '@executable_path/../../Frameworks']
  s['DEVELOPMENT_TEAM'] = runner.build_configurations.first.build_settings['DEVELOPMENT_TEAM'] if
    runner.build_configurations.first.build_settings['DEVELOPMENT_TEAM']
end

# The app: depends on the extension and embeds it, before the scripts
# Flutter adds (a later embed phase makes a build cycle).
runner.add_dependency(ext) unless runner.dependencies.any? { |d| d.target == ext }
embed = runner.copy_files_build_phases.find { |p| p.name == 'Embed Foundation Extensions' }
unless embed
  embed = project.new(Xcodeproj::Project::Object::PBXCopyFilesBuildPhase)
  embed.name = 'Embed Foundation Extensions'
  embed.symbol_dst_subfolder_spec = :plug_ins
  frameworks_at = runner.build_phases.index(runner.frameworks_build_phase)
  runner.build_phases.insert(frameworks_at + 1, embed)
end
unless embed.files_references.include?(ext.product_reference)
  bf = embed.add_file_reference(ext.product_reference, true)
  bf.settings = { 'ATTRIBUTES' => ['RemoveHeadersOnCopy'] }
end

runner_group = project.main_group.children.find { |g| g.display_name == 'Runner' }
runner.build_configurations.each do |c|
  c.build_settings['CODE_SIGN_ENTITLEMENTS'] = 'Runner/Runner.entitlements'
end
file_ref(runner_group, 'Runner.entitlements')

app_strings = runner_group.children.find { |c| c.display_name == 'Localizable.strings' }
unless app_strings
  app_strings = runner_group.new_variant_group('Localizable.strings')
  app_strings.new_reference('en.lproj/Localizable.strings').name = 'en'
end
runner.resources_build_phase.add_file_reference(app_strings, true)
ring = file_ref(runner_group, 'churr-ring.wav')
runner.resources_build_phase.add_file_reference(ring, true)

project.save
puts "#{NAME}: target, embed phase, entitlements and resources in place."
