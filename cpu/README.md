# 6502 testing
<b>NOTES:</b> to download new versions of the 6502 functional test:

wget https://github.com/Klaus2m5/6502_65C02_functional_tests/raw/master/bin_files/6502_functional_test.bin
<br>
wget https://github.com/Klaus2m5/6502_65C02_functional_tests/raw/master/bin_files/6502_functional_test.lst
<br>

<p>
To debug failures for 6502_function_test.bin read the listing file to see commented understanding of what
failed and then the execution buffer to see how you got to this state.

<p>
To debug nestest.nes the nestest.log file has the expected execution trace and values. The test will stop
if anything here is incorrect including cycle counts. The cycles in the log file are based on 341 pixel clocks
which is 3x the speed of the cpu. So the test multiplies the current total count by 3 and mods 341 to get the
value to use for comparison.
