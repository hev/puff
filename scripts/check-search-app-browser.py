import argparse, json, os, threading, subprocess, time
from contextlib import contextmanager
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from playwright.sync_api import sync_playwright
parser=argparse.ArgumentParser(description='Browser acceptance against an isolated Turbopuffer-shaped endpoint; no real credentials or datasets.')
parser.add_argument('--binary',type=Path,default=Path('./tpuff'))
parser.add_argument('--ui-dir',type=Path)
args=parser.parse_args()
requests=[]
schema={"body":{"type":"string","full_text_search":True},"title":{"type":"string"},"summary":{"type":"string","full_text_search":True},"category":{"type":"string"},"active":{"type":"bool"},"size":{"type":"uint"},"date":{"type":"datetime"}}
class API(BaseHTTPRequestHandler):
 def log_message(self,*args): pass
 def reply(self, value):
  data=json.dumps(value).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 def do_GET(self):
  assert self.path.endswith('/metadata'),self.path
  self.reply({'schema':schema})
 def do_POST(self):
  assert self.path=='/v2/namespaces/notes/query',self.path
  q=json.loads(self.rfile.read(int(self.headers['Content-Length'])));requests.append(q)
  if q.get('aggregate_by'):self.reply({'aggregation_groups':[{'category':'Guide','count':4},{'category':'Report','count':2}]});return
  if q.get('rank_by',[None,None,None])[2:]==['slow']:
   time.sleep(2)
  self.reply({'rows':[{'id':18446744073709551615,'title':'A source document','body':'A live result through the Go SDK','active':False,'size':18446744073709551615,'date':'2026-09-10T00:00:00Z','category':'Guide'}]})
server=ThreadingHTTPServer(('127.0.0.1',0),API);threading.Thread(target=server.serve_forever,daemon=True).start()
env=dict(os.environ,NO_PROXY='127.0.0.1,localhost',no_proxy='127.0.0.1,localhost',TURBOPUFFER_API_KEY='test-only-fixture-key',TURBOPUFFER_BASE_URL='http://127.0.0.1:'+str(server.server_port),TURBOPUFFER_REGION='aws-us-east-1')
command=[str(args.binary.resolve()),'serve','-n','notes','--no-open']
if args.ui_dir: command+=['--ui-dir',str(args.ui_dir.resolve())]
@contextmanager
def app(*flags):
 proc=subprocess.Popen(command+list(flags),env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 try:
  url=proc.stdout.readline().strip();assert url.startswith('http://127.0.0.1:'),(url,proc.stderr.read())
  yield url
 finally:
  proc.terminate()
  try:proc.wait(timeout=7)
  except subprocess.TimeoutExpired:proc.kill();raise
  assert proc.returncode==0,(proc.returncode,proc.stderr.read())
try:
 with sync_playwright() as p:
  browser=p.chromium.launch()
  with app() as url:
   page=browser.new_page(viewport={'width':1280,'height':950});errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
   page.add_init_script("localStorage.setItem('hevlayer-ui-theme-v1', JSON.stringify({palette:'hev',appearance:'light'}))")
   page.goto(url);page.locator('#liveChips .live-filter').first.wait_for();assert requests==[],requests
   assert '#session=' not in page.url
   assert page.locator('html').get_attribute('data-palette')=='turbopuffer'
   assert page.locator('html').get_attribute('data-appearance')=='dark'
   assert page.locator('#paletteChoice,#appearanceChoice,#liveField,#liveLayout,#liveLimit,#liveFilter,#liveApply,#liveBrowse,#liveStop').count()==0
   assert page.locator('#liveChips .live-filter').count()==5
   assert page.locator('#liveSearch button').all_text_contents()==['Search','Clear']
   search=page.locator('#liveSearch button[type=submit]');clear=page.locator('#liveSearch [data-clear]')
   page.locator('#liveSearch input').fill('evidence');search.click();page.get_by_text('Results ready',exact=True).wait_for()
   assert requests[-1]['rank_by']==['body','BM25','evidence'];assert requests[-1]['top_k']==25
   assert '18446744073709551615' in page.locator('#liveRows').inner_text()
   page.locator('[data-edit=active]').click();page.locator('[data-boolean=false]').click();search.click();page.get_by_text('Results ready',exact=True).wait_for()
   assert requests[-1]['filters']==['active','Eq',False],requests[-1]
   page.locator('[data-edit=size]').click();page.locator('[data-range-mode=above]').click();page.locator('#numberLow').fill('18446744073709551614');search.click();page.get_by_text('Results ready',exact=True).wait_for()
   assert ['size','Gte',18446744073709551614] in requests[-1]['filters'][1],requests[-1]
   page.locator('[data-edit=category]').click();page.locator('.vs-option').first.wait_for();assert 'aggregate_by' in requests[-1]
   page.locator('.vs-option').first.click();search.click();page.get_by_text('Results ready',exact=True).wait_for()
   assert ['category','In',['Guide']] in requests[-1]['filters'][1]
   page.locator('.result-title').first.click();page.locator('dialog').wait_for();assert '18446744073709551615' in page.locator('dialog').inner_text();page.keyboard.press('Escape');page.locator('dialog').wait_for(state='detached')
   page.screenshot(path='/tmp/tpuff-simple-desktop.png',full_page=True)
   page.set_viewport_size({'width':390,'height':844});assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
   page.screenshot(path='/tmp/tpuff-simple-mobile.png',full_page=True)
   with page.expect_download() as download:page.locator('summary').click();page.locator('#liveSave').click()
   data=Path(download.value.path()).read_text();assert 'test-only-fixture-key' not in data;assert json.loads(data)['limit']==25
   page.locator('#liveSearch input').fill('slow');search.click();page.get_by_text('Searching…',exact=True).wait_for();clear.click()
   page.wait_for_timeout(2200);assert page.locator('#liveRows').inner_text()==''
   assert page.locator('#liveSearch input').input_value()==''
   assert page.locator('#liveChips .live-filter').count()==5
   assert page.locator('#liveChips [data-active=true]').count()==0
   search.click();page.get_by_text('Results ready',exact=True).wait_for();assert requests[-1]['rank_by']==['id','asc'];assert not requests[-1].get('filters')
   assert not errors,errors
   page.close()
  # Options configure the rendered app and outgoing requests without browser pickers.
  with app('--fts','summary','--limit','7','--layout','table','--palette','hev','--appearance','light') as url:
   page=browser.new_page();page.goto(url);page.locator('#liveSearch input').fill('chosen field');page.locator('button[type=submit]').click();page.get_by_text('Results ready',exact=True).wait_for()
   assert requests[-1]['rank_by']==['summary','BM25','chosen field'];assert requests[-1]['top_k']==7
   assert page.locator('#liveRows table').count()==1
   assert page.locator('html').get_attribute('data-palette')=='hev';assert page.locator('html').get_attribute('data-appearance')=='light'
   page.close()
  schema['body'].pop('full_text_search');schema['summary'].pop('full_text_search')
  with app('--layout','cards') as url:
   page=browser.new_page();page.goto(url);page.locator('#liveSearch').wait_for();assert page.locator('#liveSearch input').is_disabled()
   page.locator('button[type=submit]').click();page.get_by_text('Results ready',exact=True).wait_for();assert requests[-1]['rank_by']==['id','asc']
   assert page.locator('#liveRows .cards').count()==1
   page.close()
  browser.close()
  print('Browser passed: Search/Clear, all filters visible, defaults and CLI overrides, typed predicates, facets, layouts, mobile, export, cancellation, and browse without a full-text index.')
finally:
 server.shutdown()
