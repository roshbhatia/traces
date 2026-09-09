#!/usr/bin/env python3
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
path = ROOT / '.goreleaser.yaml'
original = path.read_text()
text = re.sub(r'  # BEGIN GENERATED EXTRAS .*?  # END GENERATED EXTRAS\n', '', original, flags=re.S)
index = json.loads((ROOT / 'package-index.json').read_text())
build_section = text.split('builds:\n', 1)[1].split('\narchives:', 1)[0]
known = {}
for match in re.finditer(r'^  - id: (\S+)\n(.*?)(?=^  - id:|\Z)', build_section, re.M | re.S):
    main = re.search(r'^    main: (\S+)', match[2], re.M)
    if main:
        known[main[1].removeprefix('./')] = match[1]
builds, archives = [], []
for entry, metadata in zip(index['packages'], index['providers'], strict=True):
    extra = metadata['name']
    directory = ROOT / 'extras' / extra
    if not (directory / 'main.go').is_file():
        continue
    package_path = 'extras/' + extra
    identifier = known.get(package_path, entry['binary'])
    if package_path not in known:
        build = {
            'id': identifier, 'main': './' + package_path, 'binary': entry['binary'],
            'env': ['CGO_ENABLED=0'], 'flags': ['-trimpath'],
            'goos': ['darwin', 'linux'], 'goarch': ['amd64', 'arm64'],
            'ignore': [{'goos': 'darwin', 'goarch': 'amd64'}],
        }
        if (directory / 'go.mod').is_file():
            build.update(dir=package_path, main='.')
        builds.append(build)
    archive_id = entry['kind'] + '-' + extra
    if re.search(r'^  - id: ' + re.escape(archive_id) + r'\s*$', text, re.M):
        continue
    name = entry['archive'].removesuffix('.tar.gz')
    for field, template in [('version', 'Version'), ('os', 'Os'), ('arch', 'Arch')]:
        name = name.replace('%{' + field + '}', '{{ .' + template + ' }}')
    files = ['LICENSE', {'src': 'extras/' + extra + '/README.md', 'dst': 'README.md'}]
    for shared in entry.get('share', []):
        extension = pathlib.Path(shared).suffix
        files.append({'src': 'extras/' + extra + '/provider' + extension, 'dst': shared})
    if entry.get('npm_runtime'):
        for filename in ('package.json', 'package-lock.json'):
            files.append({'src': entry['npm_runtime'] + '/' + filename, 'dst': 'runtime/' + filename})
    archives.append({'id': archive_id, 'ids': [identifier], 'formats': ['tar.gz'], 'name_template': name, 'files': files})
for section, entries in [('builds', builds), ('archives', archives)]:
    block = '  # BEGIN GENERATED EXTRAS ' + section.upper() + '\n'
    block += ''.join('  - ' + json.dumps(entry) + '\n' for entry in entries)
    block += '  # END GENERATED EXTRAS\n'
    text = text.replace(section + ':\n', section + ':\n' + block, 1)
text = text.replace('ids: [gate, review, notes, edit-event]', 'ids: [gate]')
if not re.search(r'^release:', text, re.M):
    text += '\nrelease:\n  extra_files:\n    - glob: package-index.json\n'
if '--check' in sys.argv:
    if original != text:
        raise SystemExit('stale extra release configuration')
else:
    path.write_text(text)
