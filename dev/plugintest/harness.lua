package.path = '/tmp/pt/?/init.lua;' .. package.path
local D = '/tmp/pt/shotpanel'
local function cp(src) local i=io.open(src,'rb'); local b=i:read('a'); i:close(); local o=io.open(D..'/state.txt','r+b') or io.open(D..'/state.txt','wb'); o:seek('set',0); o:write(b); o:flush(); o:close() end
os.remove(D..'/state.txt')
-- types: token -> {type, player}
local T = {P1_BUTTON1={1,0}, P1_JOYSTICK_UP={2,0}, START1={3,0}, P2_JOYSTICK_LEFT={4,1}, P2_JOYSTICK_RIGHT={5,1}, COIN1={6,0}}
local rev = {}
for k,v in pairs(T) do rev[v[1]..':'..v[2]] = k end
local mk = function(name, tok) local t=T[tok]; return {name=name, type=t[1], player=t[2], is_analog=false, v=0,
  set_value=function(self,x) self.v=x end, clear_value=function(self) self.v=0 end} end
local F = {b1=mk('B1','P1_BUTTON1'), up=mk('Up','P1_JOYSTICK_UP'), st=mk('Start','START1'), l2=mk('L2','P2_JOYSTICK_LEFT'), r2=mk('R2','P2_JOYSTICK_RIGHT'), coin=mk('Coin','COIN1')}
local an = {name='dial', type=99, player=0, is_analog=true}
local notif = {}
_G.manager = { plugins = {}, machine = { ioport = {
  ports = { IN0 = { fields = { B1=F.b1, Up=F.up, Start=F.st, Coin=F.coin, dial=an } }, IN1 = { fields = { L2=F.l2, R2=F.r2 } } },
  screens = nil,
  input_type_to_token = function(self, ty, pl) return rev[ty..':'..(pl or 0)] or ('TYPE_OTHER('..ty..','..(pl or 0)..')') end } } }
manager.machine.screens = { [':screen'] = { refresh = 57.613 } }
_G.emu = { register_menu=function(cb, pop, name) notif.menu = pop; notif.menuname = name end, romname=function() return 'raiden' end,
  add_machine_reset_notifier=function(f) notif.reset=f return setmetatable({}, {__gc=function() notif.reset=function() error('reset notifier was garbage-collected') end end}) end,
  add_machine_frame_notifier=function(f) notif.frame=f return setmetatable({}, {__gc=function() notif.frame=function() error('frame notifier was garbage-collected') end end}) end,
  add_machine_stop_notifier=function(f) notif.stop=f return setmetatable({}, {__gc=function() notif.stop=function() error('stop notifier was garbage-collected') end end}) end }
local p = require('shotpanel'); p.set_folder(D); p.startplugin()
collectgarbage(); collectgarbage()  -- MAME と同じく、捨てられた登録は外れる
notif.reset()
local r = io.open(D..'/running.txt'); assert(r and r:read('l')=='raiden', 'running.txt'); r:close()
local function st() return string.format('b1=%d up=%d st=%d l2=%d r2=%d coin=%d', F.b1.v, F.up.v, F.st.v, F.l2.v, F.r2.v, F.coin.v) end
local log = {}
local function fr(tag) notif.frame(); log[#log+1] = tag..' '..st(); print(log[#log]) end
fr('nofile')       -- no state file yet
cp('/tmp/pt/s1.txt')
for i=1,60 do notif.frame() end  -- waits 60 frames before reopening
fr('s1-a'); fr('s1-b'); fr('s1-c'); fr('s1-d')
cp('/tmp/pt/s2.txt')
for i=1,8 do fr('s2-'..i) end
-- torn write: header seq 9, footer old -> keep state
local o=io.open(D..'/state.txt','r+b'); o:seek('set',0); o:write('SHOTPANEL 1 9\n'); o:close()
fr('torn')
cp('/tmp/pt/s3.txt'); fr('s3-empty')
cp('/tmp/pt/s1.txt') -- same seq 1 as before? seq differs from 3 -> accepted
fr('s1again')
for i=1,185 do notif.frame() end
fr('stale')
local a = io.open(D..'/ack.txt'); local ack = a:read('l'); a:close(); print('ack:', ack)
assert(ack:match('^raiden 1 %d+ 57%.613$'), 'ack format')
for _, it in ipairs(notif.menu()) do print('menu:', it[1], it[2]) end
assert(notif.menuname == 'ShotPanel')
notif.stop()
assert(io.open(D..'/running.txt')==nil, 'running.txt removed')
assert(io.open(D..'/ack.txt')==nil, 'ack.txt removed')
print('OK')
