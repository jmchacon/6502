# 6502 testing
<b>NOTES:</b>

<p>
To download new versions of the 6502 functional test:

wget https://github.com/Klaus2m5/6502_65C02_functional_tests/raw/master/bin_files/6502_functional_test.bin
<br>
wget https://github.com/Klaus2m5/6502_65C02_functional_tests/raw/master/bin_files/6502_functional_test.lst
<br>

To debug failures for 6502_function_test.bin read the listing file to see commented understanding of what
failed and then the execution buffer to see how you got to this state.

<p>
dadc.prg, dincsbc-deccmp.prg, dincsbc.prg, droradc.prg, dsbc-cmp-flags.prg, dsbc.prg, sbx.prg and  vsbx.prg
are all extracted from 6502_cpu.txt which was downloaded from https://nesdev.com/6502_cpu.txt

These are all Commodore 64 style PRG BASIC+embedded assembly that runs with a SYS XXX BASIC instruction.
See other utilities such as ../convertprg to convert these into ROM images for testing.

<p>
nestest.txt, nestest.log and nestest.nes all come from http://www.qmtpro.com/~nes/misc or
https://github.com/christopherpow/nes-test-roms/tree/master/other

I don't know where the originals came from at this point beyond attributions in nestest.txt

To debug nestest.nes the nestest.log file has the expected execution trace and values. The test will stop
if anything here is incorrect including cycle counts. The cycles in the log file are based on 341 pixel clocks
which is 3x the speed of the cpu. So the test multiplies the current total count by 3 and mods 341 to get the
value to use for comparison.

<p>
bcd_test.asm is hand extracted assembly from http://www.6502.org/tutorials/decimal_mode.html#B
and then using ../hand_asm converted into a test ROM file.

