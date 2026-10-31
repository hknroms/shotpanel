-- license:MIT
-- ShotPanel（連射パネル設定）用のプラグイン。
-- ShotPanel が state.txt に書いた「押している入力と連射のパターン」を毎フレーム読み、
-- MAME のフレームに合わせてボタン・レバーを押し離しする（PC が重くても、ゲームの画面の回数が60でなくてもずれない）。
--
-- state.txt の形（ShotPanel が 4096 バイト固定で上書きする）:
--   SHOTPANEL 1 <seq>
--   R <番号> <押すフレーム数> <離すフレーム数> <行き先,...> <交互の行き先,... または ->
--   E <seq>
-- 行き先は MAME の入力名（P1_BUTTON1・P2_JOYSTICK_UP・START1・COIN1・SERVICE1 など）。
-- 離すフレーム数が 0 のときは押しっぱなし。番号が新しくなったら1フレーム目（押す）から数え直す。
local exports = {
	name = 'shotpanel',
	version = '1.0.0',
	description = 'ShotPanel: frame-synced rapid fire and button mapping',
	license = 'MIT',
	author = { name = 'hknroms' } }

local shotpanel = exports
local folder = nil

-- 通知の登録（add_machine_*_notifier が返すもの）は、捨てられる（ガベージコレクションされる）と外れてしまう。
-- なので、ずっと残るここ（モジュールの変数）に持っておく。
local subscriptions = {}

function shotpanel.set_folder(path)
	folder = path
end

function shotpanel.startplugin()
	if not folder then
		folder = manager.plugins['shotpanel'] and manager.plugins['shotpanel'].directory or 'plugins/shotpanel'
	end

	local fh = nil        -- state.txt（開いたままにして、毎フレーム先頭から読み直す）
	local wait = 0        -- state.txt が無いとき、次に開いてみるまでのフレーム数
	local fields = {}     -- 入力名 → { ioport_field, ... }
	local reqs = {}       -- 番号 → { on, off, a, b, c }
	local lastseq = nil
	local stale = 0       -- seq が変わらないフレーム数（ShotPanel が止まったら離す）
	local active = {}     -- いま押している入力名
	local started = false -- このゲームで準備（start）が済んだか
	local frames = 0      -- ゲームを始めてからのフレーム数
	local rom = ''
	local hz = 0          -- ゲームの画面の回数（1秒あたり。例：ネオジオは約59.19）

	local function split(s)
		local t = {}
		if s and s ~= '-' then
			for x in string.gmatch(s, '[^,]+') do
				t[#t + 1] = x
			end
		end
		return t
	end

	local function build_fields()
		fields = {}
		local im = manager.machine.ioport
		for _, port in pairs(im.ports) do
			for _, f in pairs(port.fields) do
				if not f.is_analog then
					local ok, tok = pcall(function () return im:input_type_to_token(f.type, f.player) end)
					if not ok or not tok or tok:find('^TYPE_OTHER') then
						ok, tok = pcall(function () return im:input_type_to_token(f.type) end)
					end
					if ok and tok and not tok:find('^TYPE_OTHER') then
						local l = fields[tok]
						if not l then
							l = {}
							fields[tok] = l
						end
						l[#l + 1] = f
					end
				end
			end
		end
	end

	local function release_all()
		for tok in pairs(active) do
			for _, f in ipairs(fields[tok] or {}) do
				f:clear_value()
			end
		end
		active = {}
		reqs = {}
	end

	local function mark(text)
		local f = io.open(folder .. '/running.txt', 'w')
		if f then
			f:write(text)
			f:close()
		end
	end

	local function read_state()
		if not fh then
			if wait > 0 then
				wait = wait - 1
				return
			end
			fh = io.open(folder .. '/state.txt', 'rb')
			if not fh then
				wait = 60
				return
			end
		end
		fh:seek('set', 0)
		local s = fh:read(4096)
		if not s then
			stale = stale + 1
			return
		end
		local seq = s:match('^SHOTPANEL 1 (%d+)\n')
		if not seq or not s:find('\nE ' .. seq .. '\n', 1, true) then
			stale = stale + 1 -- 書き換えの途中を読んだ（次のフレームで読み直す）
			return
		end
		if seq == lastseq then
			stale = stale + 1
			return
		end
		lastseq = seq
		stale = 0
		local new = {}
		for n, on, off, a, b in s:gmatch('\nR (%d+) (%d+) (%d+) (%S+) (%S+)') do
			local r = reqs[n] or { c = 0 }
			r.on = math.max(1, tonumber(on))
			r.off = tonumber(off)
			r.a = split(a)
			r.b = split(b)
			new[n] = r
		end
		reqs = new
	end

	-- ack.txt：ShotPanel への返事（30フレームごと）。「ROM名 受け取ったseq フレーム数 画面の回数」
	-- ShotPanel はこれで「つながった」と「1秒あたりのフレーム数（MAMEの速さ）」を知る。
	local function write_ack()
		local f = io.open(folder .. '/ack.txt', 'w')
		if f then
			f:write(rom .. ' ' .. (lastseq or '0') .. ' ' .. frames .. ' ' .. string.format('%.3f', hz) .. '\n')
			f:close()
		end
	end

	local start

	local function process_frame()
		if not started then
			start() -- リセットの通知が来なかったときも、ここで準備する
		end
		frames = frames + 1
		if frames % 30 == 0 then
			write_ack()
		end
		read_state()
		if stale > 180 then
			-- ShotPanel から3秒ほど何も来ない（終了した・止まった）ので全部離す
			if next(reqs) then
				reqs = {}
			end
		end
		local want = {}
		for _, r in pairs(reqs) do
			local per = r.on + r.off
			local push = true
			local t = r.a
			if r.off > 0 then
				push = (r.c % per) < r.on
				if #r.b > 0 and ((r.c // per) % 2) == 1 then
					t = r.b
				end
			end
			if push then
				for _, tok in ipairs(t) do
					want[tok] = true
				end
			end
			r.c = r.c + 1
		end
		for tok in pairs(want) do
			for _, f in ipairs(fields[tok] or {}) do
				f:set_value(1)
			end
		end
		for tok in pairs(active) do
			if not want[tok] then
				for _, f in ipairs(fields[tok] or {}) do
					f:clear_value()
				end
			end
		end
		active = want
	end

	start = function ()
		started = true
		build_fields()
		lastseq = nil
		stale = 0
		reqs = {}
		active = {}
		frames = 0
		rom = emu.romname()
		hz = 0
		pcall(function ()
			for _, scr in pairs(manager.machine.screens) do
				hz = scr.refresh
				break
			end
		end)
		mark(rom .. '\n')
		write_ack()
	end

	local function stop()
		started = false
		release_all()
		if fh then
			fh:close()
			fh = nil
		end
		os.remove(folder .. '/running.txt')
		os.remove(folder .. '/ack.txt')
	end

	-- ゲーム中のメニュー（Tab →「Plugin Options」→「ShotPanel」）に状態を出す
	local function menu_populate()
		local linked = lastseq ~= nil and stale < 60
		local n = 0
		for _ in pairs(reqs) do n = n + 1 end
		return {
			{ 'ShotPanel', '', 'off' },
			{ 'Plugin', 'loaded', 'off' },
			{ 'Link to ShotPanel', linked and 'OK' or 'waiting', 'off' },
			{ 'Inputs from ShotPanel', tostring(n), 'off' } }
	end
	local function menu_callback(index, event)
		return false
	end
	if emu.register_menu then
		emu.register_menu(menu_callback, menu_populate, 'ShotPanel')
	end

	subscriptions.reset = emu.add_machine_reset_notifier(start)
	subscriptions.frame = emu.add_machine_frame_notifier(process_frame)
	subscriptions.stop = emu.add_machine_stop_notifier(stop)
end

return exports
