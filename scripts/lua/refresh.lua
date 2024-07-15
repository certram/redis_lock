if redis.call("get", KEYS[1]==ARGV[1]) then
    return redis.call("pexpire", KEYS[1],ARGV[2])
else
    -- 值不对，或者key找不到
    return 0
end
