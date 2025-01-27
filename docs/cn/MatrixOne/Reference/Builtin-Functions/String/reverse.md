# **REVERSE()**

## **函数说明**

将str字符串中的字符顺序翻转输出。

## **函数语法**

```
> REVERSE(str)
```

## **参数释义**

|  参数   | 说明  |
|  ----  | ----  |
| Str | 必要参数. 需要翻转的字符串. CHAR与VARCHAR类型均可. |

## **示例**

```SQL
> drop table if exists t1;
> create table t1(a varchar(12),c char(30));
> insert into t1 values('sdfad  ','2022-02-02 22:22:22');
> insert into t1 values('  sdfad  ','2022-02-02 22:22:22');
> insert into t1 values('adsf  sdfad','2022-02-02 22:22:22');
> insert into t1 values('    sdfad','2022-02-02 22:22:22');
> select reverse(a),reverse(c) from t1;
+-------------+---------------------+
| reverse(a)  | reverse(c)          |
+-------------+---------------------+
|   dafds     | 22:22:22 20-20-2202 |
|   dafds     | 22:22:22 20-20-2202 |
| dafds  fsda | 22:22:22 20-20-2202 |
| dafds       | 22:22:22 20-20-2202 |
+-------------+---------------------+
> select a from t1 where reverse(a) like 'daf%';
+-------------+
| a           |
+-------------+
| adsf  sdfad |
|     sdfad   |
+-------------+
> select reverse(a) reversea,reverse(reverse(a)) normala from t1;
+-------------+-------------+
| reversea    | normala     |
+-------------+-------------+
|   dafds     | sdfad       |
|   dafds     |   sdfad     |
| dafds  fsda | adsf  sdfad |
| dafds       |     sdfad   |
+-------------+-------------+
```

## **限制**

MatrixOne目前只支持在查询表的时候使用函数，不支持单独使用函数。
