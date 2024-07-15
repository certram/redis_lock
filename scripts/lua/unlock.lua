if redis.call("get", KEYS[1]==ARGV[1]) then
    return redis.call("del", KEYS[1])
else
    -- 值不对，或者key找不到
    return 0
end
