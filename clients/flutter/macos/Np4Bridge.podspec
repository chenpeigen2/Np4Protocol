Pod::Spec.new do |s|
  s.name             = 'Np4Bridge'
  s.version          = '0.1.0'
  s.summary          = 'Native np4 mixnet bridge for the Flutter client.'
  s.description      = 'Go-built np4 bridge (libnp4bridge.dylib); see go/cmd/np4bridge.'
  s.homepage         = 'https://example.invalid/np4'
  s.license          = { :type => 'MIT' }
  s.authors          = { 'Np4Protocol' => 'dev@np4.invalid' }
  s.source           = { :path => '.' }
  s.platform         = :osx, '10.15'
  s.vendored_libraries = 'native/libnp4bridge.dylib'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
